package common

import "context"

type qualityRequestKey struct{}

// QualityRequest is in-process metadata, not accepted from HTTP headers or JSON.
type QualityRequest struct {
	Group    string
	ResultID int64
	Stage    string
}

func WithQualityRequest(ctx context.Context, request QualityRequest) context.Context {
	return context.WithValue(ctx, qualityRequestKey{}, request)
}

func GetQualityRequest(ctx context.Context) (QualityRequest, bool) {
	request, ok := ctx.Value(qualityRequestKey{}).(QualityRequest)
	return request, ok
}
