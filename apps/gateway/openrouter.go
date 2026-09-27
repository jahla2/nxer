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
	"time"
)

type OpenRouterProvider struct {
	baseURL string
	apiKey string
	client *http.Client
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
		if m.ID==""||m.Pricing.Prompt!="0"||m.Pricing.Completion!="0"||!containsString(m.Architecture.OutputModalities,"text"){continue}
		models=append(models,Model{ID:m.ID,Object:"model",OwnedBy:"openrouter",DisplayName:m.Name,ContextLength:m.ContextLength,Status:"active",Free:true,Capabilities:map[string]bool{"text":true}})
	}
	catalog.Replace(models)
	return nil
}

func (p *OpenRouterProvider) Chat(ctx context.Context,req *ChatCompletionRequest,w http.ResponseWriter)*APIError{
	upstreamReq:=*req
	if upstreamReq.Model=="auto-free"{upstreamReq.Model="openrouter/free"}
	payload,err:=json.Marshal(upstreamReq)
	if err!=nil{return newAPIError(500,"server_error","NEXORA_INTERNAL_ERROR","Unable to encode request.")}
	upReq,err:=p.newRequest(ctx,http.MethodPost,"/chat/completions",bytes.NewReader(payload))
	if err!=nil{return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Unable to create upstream request.")}
	if req.Stream{upReq.Header.Set("Accept","text/event-stream")}
	resp,err:=p.client.Do(upReq)
	if err!=nil{
		if errors.Is(err,context.DeadlineExceeded){return newAPIError(504,"upstream_error","NEXORA_UPSTREAM_TIMEOUT","Upstream provider timed out.")}
		return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Upstream provider request failed.")
	}
	defer resp.Body.Close()
	if resp.StatusCode<200||resp.StatusCode>=300{return mapUpstreamStatus(resp.StatusCode)}
	if req.Stream{return p.streamResponse(ctx,resp,w)}
	return p.jsonResponse(resp,w)
}

func (p *OpenRouterProvider) jsonResponse(resp *http.Response,w http.ResponseWriter)*APIError{
	body,err:=io.ReadAll(io.LimitReader(resp.Body,8<<20))
	if err!=nil{return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_ERROR","Unable to read upstream response.")}
	var payload any
	if err:=json.Unmarshal(body,&payload);err!=nil{return newAPIError(502,"upstream_error","NEXORA_UPSTREAM_INVALID_RESPONSE","Upstream provider returned an invalid response.")}
	writeJSON(w,http.StatusOK,payload)
	return nil
}

func (p *OpenRouterProvider) streamResponse(ctx context.Context,resp *http.Response,w http.ResponseWriter)*APIError{
	flusher,ok:=w.(http.Flusher)
	if !ok{return newAPIError(500,"server_error","NEXORA_STREAMING_UNSUPPORTED","Streaming is unavailable.")}
	w.Header().Set("Content-Type","text/event-stream")
	w.Header().Set("Cache-Control","no-cache")
	w.Header().Set("X-Accel-Buffering","no")
	w.WriteHeader(http.StatusOK)
	reader:=bufio.NewReaderSize(resp.Body,32*1024)
	for{
		line,err:=reader.ReadBytes('\n')
		if len(line)>0{
			select{case <-ctx.Done():return nil;default:}
			if _,writeErr:=w.Write(line);writeErr!=nil{return nil}
			flusher.Flush()
		}
		if err!=nil{
			if errors.Is(err,io.EOF){return nil}
			return nil
		}
	}
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
