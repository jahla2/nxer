package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type IdempotencyGuard struct{ client *redis.Client; ttl time.Duration }

func NewIdempotencyGuard(client *redis.Client) *IdempotencyGuard {
	return &IdempotencyGuard{client:client,ttl:time.Duration(getenvInt("IDEMPOTENCY_TTL_HOURS",24))*time.Hour}
}

func requestDigest(req *ChatCompletionRequest) (string,error) {
	body,err:=json.Marshal(req); if err!=nil{return "",err}
	sum:=sha256.Sum256(body); return hex.EncodeToString(sum[:]),nil
}

func (g *IdempotencyGuard) Begin(r *http.Request,p *APIKeyPrincipal,req *ChatCompletionRequest)*APIError {
	key:=strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key=="" { return nil }
	if len(key)>128 { return newAPIError(422,"invalid_request_error","NEXORA_INVALID_IDEMPOTENCY_KEY","Idempotency-Key is too long.") }
	digest,err:=requestDigest(req); if err!=nil{return newAPIError(500,"server_error","NEXORA_INTERNAL_ERROR","Unable to fingerprint request.")}
	redisKey:="nxa:idempotency:"+p.ID+":"+key
	created,err:=g.client.SetNX(r.Context(),redisKey,digest,g.ttl).Result()
	if err!=nil{return newAPIError(503,"service_unavailable","NEXORA_IDEMPOTENCY_UNAVAILABLE","Idempotency protection is unavailable.")}
	if created{return nil}
	existing,err:=g.client.Get(r.Context(),redisKey).Result()
	if err!=nil{return newAPIError(503,"service_unavailable","NEXORA_IDEMPOTENCY_UNAVAILABLE","Idempotency protection is unavailable.")}
	if existing!=digest{return newAPIError(409,"conflict_error","NEXORA_IDEMPOTENCY_CONFLICT","Idempotency-Key was already used with a different request.")}
	return newAPIError(409,"conflict_error","NEXORA_DUPLICATE_REQUEST","This request has already been started.")
}
