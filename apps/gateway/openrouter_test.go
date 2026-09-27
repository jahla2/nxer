package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncFreeModelsFiltersPaidAndNonTextModels(t *testing.T){
	server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path!="/models"{t.Fatalf("unexpected path %s",r.URL.Path)}
		w.Header().Set("Content-Type","application/json")
		_,_=w.Write([]byte(`{"data":[
			{"id":"free/text","name":"Free Text","context_length":8192,"pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["text"]}},
			{"id":"paid/text","name":"Paid","pricing":{"prompt":"0.1","completion":"0"},"architecture":{"output_modalities":["text"]}},
			{"id":"free/image","name":"Image","pricing":{"prompt":"0","completion":"0"},"architecture":{"output_modalities":["image"]}}
		]}`))
	}))
	defer server.Close()
	p:=&OpenRouterProvider{baseURL:server.URL,apiKey:"secret",client:server.Client()}
	catalog:=NewModelCatalog()
	if err:=p.SyncFreeModels(context.Background(),catalog);err!=nil{t.Fatal(err)}
	if _,ok:=catalog.Get("free/text");!ok{t.Fatal("expected free text model")}
	if _,ok:=catalog.Get("paid/text");ok{t.Fatal("paid model must not be exposed")}
	if _,ok:=catalog.Get("free/image");ok{t.Fatal("non-text model must not be exposed")}
	if _,ok:=catalog.Get("auto-free");!ok{t.Fatal("auto-free alias must remain available")}
}

func TestChatMapsAutoFreeAndReturnsJSON(t *testing.T){
	server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("Authorization")!="Bearer secret"{t.Fatal("missing provider authorization")}
		body:=make([]byte,r.ContentLength);_,_=r.Body.Read(body)
		if !strings.Contains(string(body),`"model":"openrouter/free"`){t.Fatalf("unexpected body %s",body)}
		w.Header().Set("Content-Type","application/json")
		_,_=w.Write([]byte(`{"id":"upstream-id","object":"chat.completion","choices":[]}`))
	}))
	defer server.Close()
	p:=&OpenRouterProvider{baseURL:server.URL,apiKey:"secret",client:server.Client()}
	req:=&ChatCompletionRequest{Model:"auto-free",Messages:[]ChatMessage{{Role:"user",Content:"hello"}}}
	rec:=httptest.NewRecorder()
	if apiErr:=p.Chat(context.Background(),req,rec);apiErr!=nil{t.Fatalf("unexpected error %#v",apiErr)}
	if rec.Code!=200{t.Fatalf("expected 200, got %d",rec.Code)}
}

func TestChatMapsUpstreamRateLimitWithoutLeakingBody(t *testing.T){
	server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		w.WriteHeader(http.StatusTooManyRequests)
		_,_=w.Write([]byte("provider-secret-error-detail"))
	}))
	defer server.Close()
	p:=&OpenRouterProvider{baseURL:server.URL,apiKey:"secret",client:server.Client()}
	req:=&ChatCompletionRequest{Model:"free/text",Messages:[]ChatMessage{{Role:"user",Content:"hello"}}}
	rec:=httptest.NewRecorder()
	apiErr:=p.Chat(context.Background(),req,rec)
	if apiErr==nil||apiErr.Status!=503||apiErr.Code!="NEXORA_UPSTREAM_CAPACITY"{t.Fatalf("unexpected error %#v",apiErr)}
	if strings.Contains(apiErr.Message,"provider-secret"){t.Fatal("provider detail leaked")}
}
