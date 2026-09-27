package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"strconv"
	"time"
)

type OpenRouterProvider struct {
	baseURL string
	apiKey string
	client *http.Client
}

type ProviderMetrics struct {
	Status int
	TTFTMS *int
	LatencyMS *int
	PromptTokens *int
	CompletionTokens *int
	Completed bool
}

type upstreamModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
		Name string `json:"name"`
		ContextLength int `json:"context_length"`
		Pricing struct {
			Prompt string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		Architecture struct {
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	} `json:"data"`
}

func NewOpenRouterProvider(baseURL, apiKey string) (*OpenRouterProvider,error) {
	if strings.TrimSpace(apiKey)=="" { return nil,errors.New("OPENROUTER_API_KEY is required") }
	u,err:=url.Parse(baseURL)
	if err!=nil || u.Scheme!="https" || u.Host=="" { return nil,errors.New("UPSTREAM_BASE_URL must be a valid HTTPS URL") }
	transport:=&http.Transport{
		Proxy:http.ProxyFromEnvironment,
		DialContext:(&net.Dialer{Timeout:5*time.Second,KeepAlive:30*time.Second}).DialContext,
		ForceAttemptHTTP2:true,
		MaxIdleConns:100,
		MaxIdleConnsPerHost:20,
		IdleConnTimeout:90*time.Second,
		TLSHandshakeTimeout:5*time.Second,
		ResponseHeaderTimeout:30*time.Second,
		ExpectContinueTimeout:1*time.Second,
	}
	return &OpenRouterProvider{baseURL:strings.TrimRight(baseURL,"/"),apiKey:apiKey,client:&http.Client{Transport:transport}},nil
}

func (p *OpenRouterProvider) newRequest(ctx context.Context, method,path string,body io.Reader)(*http.Request,error){
	req,err:=http.NewRequestWithContext(ctx,method,p.baseURL+path,body)
	if err!=nil{return nil,err}
	req.Header.Set("Authorization","Bearer "+p.apiKey)
	req.Header.Set("Content-Type","application/json")
	req.Header.Set("Accept","application/json")
	return req,nil
}

func (p *OpenRouterProvider) SyncFreeModels(ctx context.Context,catalog *ModelCatalog) error {
	ctx,cancel:=context.WithTimeout(ctx,15*time.Second); defer cancel()
	req,err:=p.newRequest(ctx,http.MethodGet,"/models",nil); if err!=nil{return err}
	resp,err:=p.client.Do(req); if err!=nil{return err}
	defer resp.Body.Close()
	if resp.StatusCode<200||resp.StatusCode>=300{return errors.New("upstream model catalog request failed")}
	var payload upstreamModelsResponse
	if err:=json.NewDecoder(io.LimitReader(resp.Body,4<<20)).Decode(&payload);err!=nil{return err}
	models:=make([]Model,0,len(payload.Data)+1)
	models=append(models,Model{ID:"auto-free",Object:"model",OwnedBy:"nexora",DisplayName:"Auto Free",Status:"active",Free:true})
	for _,m:=range payload.Data{
		if m.ID==""||!zeroPrice(m.Pricing.Prompt)||!zeroPrice(m.Pricing.Completion)||!containsString(m.Architecture.OutputModalities,"text"){continue}
		models=append(models,Model{ID:m.ID,Object:"model",OwnedBy:"openrouter",DisplayName:m.Name,ContextLength:m.ContextLength,Status:"active",Free:true,Capabilities:map[string]bool{"text":true}})
	}
	catalog.Replace(models)
	return nil
}

func (p *OpenRouterProvider) Chat(ctx context.Context,req *ChatCompletionRequest,w http.ResponseWriter)(*ProviderMetrics,*APIError){
	start:=time.Now()
	upstreamReq:=*req
	if upstreamReq.Model=="auto-free"{upstreamReq.Model="openrouter/free"}
	payload,err:=json.Marshal(upstreamReq)
	if err!=nil{return metricsFromStart(start,500,false),newAPIError(500,"server_error","NEXORA_INTERNAL_ERROR","Unable to encode request.")}
	upReq,err:=p.newRequest(ctx,http.MethodPost,"/chat/completions",bytes.NewReader(payload))
	if err!=nil{return metricsFromStart(start,502,false),newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Unable to create upstream request.")}
	if req.Stream{upReq.Header.Set("Accept","text/event-stream")}
	resp,err:=p.client.Do(upReq)
	if err!=nil{
		if errors.Is(err,context.DeadlineExceeded){return metricsFromStart(start,504,false),newAPIError(504,"upstream_error","NEXORA_UPSTREAM_TIMEOUT","Upstream provider timed out.")}
		if errors.Is(err,context.Canceled){return metricsFromStart(start,499,false),nil}
		return metricsFromStart(start,502,false),newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Upstream provider request failed.")
	}
	defer resp.Body.Close()
	if resp.StatusCode<200||resp.StatusCode>=300{
		apiErr:=mapUpstreamStatus(resp.StatusCode)
		return metricsFromStart(start,apiErr.Status,false),apiErr
	}
	if req.Stream{return p.streamResponse(ctx,resp,w,start)}
	return p.jsonResponse(resp,w,start)
}

func (p *OpenRouterProvider) jsonResponse(resp *http.Response,w http.ResponseWriter,start time.Time)(*ProviderMetrics,*APIError){
	firstRead:=time.Time{}
	reader:=&firstReadRecorder{reader:resp.Body,onFirstRead:func(){firstRead=time.Now()}}
	body,err:=io.ReadAll(io.LimitReader(reader,8<<20))
	if err!=nil{return metricsFromStart(start,502,false),newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Unable to read upstream response.")}
	var payload any
	if err:=json.Unmarshal(body,&payload);err!=nil{return metricsFromStart(start,502,false),newAPIError(502,"upstream_error","NEXORA_UPSTREAM_INVALID_RESPONSE","Upstream provider returned an invalid response.")}
	metrics:=metricsFromStart(start,200,true)
	if !firstRead.IsZero(){metrics.TTFTMS=durationMillisPtr(firstRead.Sub(start))}
	metrics.PromptTokens,metrics.CompletionTokens=extractUsageTokens(payload)
	writeJSON(w,http.StatusOK,payload)
	return metrics,nil
}

func (p *OpenRouterProvider) streamResponse(ctx context.Context,resp *http.Response,w http.ResponseWriter,start time.Time)(*ProviderMetrics,*APIError){
	flusher,ok:=w.(http.Flusher)
	if !ok{return metricsFromStart(start,500,false),newAPIError(500,"server_error","NEXORA_STREAMING_UNSUPPORTED","Streaming is unavailable.")}
	w.Header().Set("Content-Type","text/event-stream")
	w.Header().Set("Cache-Control","no-cache")
	w.Header().Set("X-Accel-Buffering","no")
	w.WriteHeader(http.StatusOK)
	metrics:=&ProviderMetrics{Status:http.StatusOK}
	reader:=bufio.NewReaderSize(resp.Body,32*1024)
	firstDataSeen:=false
	for{
		line,err:=reader.ReadBytes('\n')
		if len(line)>0{
			select{
			case <-ctx.Done():
				metrics.Status=499
				metrics.Completed=false
				metrics.LatencyMS=durationMillisPtr(time.Since(start))
				return metrics,nil
			default:
			}
			if !firstDataSeen && isSSEDataLine(line){
				firstDataSeen=true
				metrics.TTFTMS=durationMillisPtr(time.Since(start))
			}
			updateStreamUsage(metrics,line)
			if _,writeErr:=w.Write(line);writeErr!=nil{
				metrics.Status=499
				metrics.Completed=false
				metrics.LatencyMS=durationMillisPtr(time.Since(start))
				return metrics,nil
			}
			flusher.Flush()
		}
		if err!=nil{
			metrics.LatencyMS=durationMillisPtr(time.Since(start))
			if errors.Is(err,io.EOF){
				metrics.Completed=true
				return metrics,nil
			}
			metrics.Status=http.StatusBadGateway
			metrics.Completed=false
			return metrics,nil
		}
	}
}

type firstReadRecorder struct{
	reader io.Reader
	onFirstRead func()
	seen bool
}

func (r *firstReadRecorder) Read(p []byte)(int,error){
	n,err:=r.reader.Read(p)
	if n>0 && !r.seen{
		r.seen=true
		if r.onFirstRead!=nil{r.onFirstRead()}
	}
	return n,err
}

func metricsFromStart(start time.Time,status int,completed bool)*ProviderMetrics{
	return &ProviderMetrics{Status:status,LatencyMS:durationMillisPtr(time.Since(start)),Completed:completed}
}

func durationMillisPtr(d time.Duration)*int{
	value:=int(d.Milliseconds())
	return &value
}

func extractUsageTokens(payload any)(*int,*int){
	root,ok:=payload.(map[string]any)
	if !ok{return nil,nil}
	usage,ok:=root["usage"].(map[string]any)
	if !ok{return nil,nil}
	return jsonNumberInt(usage["prompt_tokens"]),jsonNumberInt(usage["completion_tokens"])
}

func jsonNumberInt(value any)*int{
	switch v:=value.(type){
	case float64:
		n:=int(v)
		return &n
	case int:
		n:=v
		return &n
	case json.Number:
		if parsed,err:=strconv.Atoi(v.String());err==nil{return &parsed}
	}
	return nil
}

func isSSEDataLine(line []byte)bool{
	trimmed:=strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed,"data:"){return false}
	payload:=strings.TrimSpace(strings.TrimPrefix(trimmed,"data:"))
	return payload!="" && payload!="[DONE]"
}

func updateStreamUsage(metrics *ProviderMetrics,line []byte){
	if metrics==nil{return}
	trimmed:=strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed,"data:"){return}
	payloadText:=strings.TrimSpace(strings.TrimPrefix(trimmed,"data:"))
	if payloadText==""||payloadText=="[DONE]"{return}
	var payload any
	if err:=json.Unmarshal([]byte(payloadText),&payload);err!=nil{return}
	prompt,completion:=extractUsageTokens(payload)
	if prompt!=nil{metrics.PromptTokens=prompt}
	if completion!=nil{metrics.CompletionTokens=completion}
}

func mapUpstreamStatus(status int)*APIError{
	switch status{
	case 429:return newAPIError(503,"service_unavailable","NEXORA_UPSTREAM_CAPACITY","Upstream free-model capacity is temporarily unavailable.")
	case 408,504:return newAPIError(504,"upstream_error","NEXORA_UPSTREAM_TIMEOUT","Upstream provider timed out.")
	default:
		if status>=500{return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Upstream provider is temporarily unavailable.")}
		return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_REJECTED","Upstream provider rejected the request.")
	}
}

func containsString(values []string,want string)bool{for _,v:=range values{if v==want{return true}};return false}

func zeroPrice(value string) bool {
	value=strings.TrimSpace(value)
	if value=="" { return false }
	parsed,err:=strconv.ParseFloat(value,64)
	return err==nil && parsed==0
}
