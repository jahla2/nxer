package main

import (
	"testing"
)

func TestRequestDigestStable(t *testing.T){
	req:=&ChatCompletionRequest{Model:"auto-free",Messages:[]ChatMessage{{Role:"user",Content:"hello"}}}
	a,err:=requestDigest(req); if err!=nil{t.Fatal(err)}
	b,err:=requestDigest(req); if err!=nil{t.Fatal(err)}
	if a==""||a!=b{t.Fatalf("digest not stable: %q %q",a,b)}
}

func TestRequestDigestChangesWithBody(t *testing.T){
	a,_:=requestDigest(&ChatCompletionRequest{Model:"auto-free",Messages:[]ChatMessage{{Role:"user",Content:"one"}}})
	b,_:=requestDigest(&ChatCompletionRequest{Model:"auto-free",Messages:[]ChatMessage{{Role:"user",Content:"two"}}})
	if a==b{t.Fatal("different request bodies must not share digest")}
}
