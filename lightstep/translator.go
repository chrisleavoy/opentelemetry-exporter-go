package lightstep

// this file copied from https://github.com/honeycombio/opentelemetry-exporter-go/blob/master/honeycomb/translator.go

import (
	"errors"
	"time"

	"go.opentelemetry.io/otel/api/key"
	"go.opentelemetry.io/otel/sdk/resource"

	"go.opentelemetry.io/otel/api/core"

	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
	apitrace "go.opentelemetry.io/otel/api/trace"
	"go.opentelemetry.io/otel/sdk/export/trace"
)

// timestampToTime creates a Go time.Time value from a Google protobuf Timestamp.
func timestampToTime(ts *timestamp.Timestamp) (t time.Time) {
	if ts == nil {
		return
	}
	return time.Unix(ts.Seconds, int64(ts.Nanos))
}

// Get SpanKind from an OC Span_SpanKind
func oTelSpanKind(kind tracepb.Span_SpanKind) apitrace.SpanKind {
	// note that tracepb.SpanKindInternal, tracepb.SpanKindProducer and tracepb.SpanKindConsumer
	// have no equivalent OC proto type.
	switch kind {
	case tracepb.Span_SPAN_KIND_UNSPECIFIED:
		return apitrace.SpanKindUnspecified
	case tracepb.Span_SERVER:
		return apitrace.SpanKindServer
	case tracepb.Span_CLIENT:
		return apitrace.SpanKindClient
	default:
		return apitrace.SpanKindUnspecified
	}
}

// Creates an OpenTelemetry SpanContext from information in an OC Span.
// Note that the OC Span has no equivalent to TraceFlags field in the
// OpenTelemetry SpanContext type.
func spanContext(traceID []byte, spanID []byte) core.SpanContext {
	ctx := core.SpanContext{}
	if traceID != nil {
		copy(ctx.TraceID[:], traceID[:])
	}
	if spanID != nil {
		copy(ctx.SpanID[:], spanID[:])
	}
	return ctx
}

// Create []kv.KeyValue attributes from an OC *Span_Attributes
func createOTelAttributes(attributes *tracepb.Span_Attributes) []core.KeyValue {
	if attributes == nil || attributes.AttributeMap == nil {
		return nil
	}

	oTelAttrs := make([]core.KeyValue, len(attributes.AttributeMap))

	i := 0
	for k, attributeValue := range attributes.AttributeMap {
		var keyValue core.KeyValue
		k := core.Key(k)
		switch value := attributeValue.Value.(type) {
		case *tracepb.AttributeValue_StringValue:
			keyValue = k.String(attributeValueAsString(attributeValue))
		case *tracepb.AttributeValue_BoolValue:
			keyValue = k.Bool(value.BoolValue)
		case *tracepb.AttributeValue_IntValue:
			keyValue = k.Int64(value.IntValue)
		case *tracepb.AttributeValue_DoubleValue:
			keyValue = k.Float64(value.DoubleValue)
		}
		oTelAttrs[i] = keyValue
		i++
	}

	return oTelAttrs
}

// // Create []trace.Event from OC TimeEvents
func createOTelEvents(spanEvents *tracepb.Span_TimeEvents) []trace.Event {
	if spanEvents == nil {
		return nil
	}

	annotations := 0
	for _, event := range spanEvents.TimeEvent {
		if annotation := event.GetAnnotation(); annotation != nil {
			annotations++
		}
	}

	if annotations == 0 {
		return nil
	}

	events := make([]trace.Event, annotations)

	for i, event := range spanEvents.TimeEvent {
		if annotation := event.GetAnnotation(); annotation != nil {
			events[i] = trace.Event{
				Time:       timestampToTime(event.GetTime()),
				Name:       annotation.GetDescription().GetValue(),
				Attributes: createOTelAttributes(annotation.GetAttributes()),
			}
		}
	}

	return events
}

// Create Span Links (including their attributes) from an OC Span
func createSpanLinks(spanLinks *tracepb.Span_Links) []apitrace.Link {
	if spanLinks == nil {
		return nil
	}

	links := make([]apitrace.Link, len(spanLinks.Link))

	for i, link := range spanLinks.Link {
		traceLink := apitrace.Link{
			SpanContext: spanContext(link.GetTraceId(), link.GetSpanId()),
			Attributes:  createOTelAttributes(link.Attributes),
		}
		links[i] = traceLink
	}

	return links
}

func attributeValueAsString(val *tracepb.AttributeValue) string {
	if wrapper := val.GetStringValue(); wrapper != nil {
		return wrapper.GetValue()
	}

	return ""
}

func getDroppedLinkCount(links *tracepb.Span_Links) int {
	if links != nil {
		return int(links.DroppedLinksCount)
	}

	return 0
}

func getChildSpanCount(span *tracepb.Span) int {
	if count := span.GetChildSpanCount(); count != nil {
		return int(count.GetValue())
	}

	return 0
}

func getSpanName(span *tracepb.Span) string {
	if name := span.GetName(); name != nil {
		return name.GetValue()
	}

	return ""
}

func spanResource(span *tracepb.Span) *resource.Resource {
	if span.Resource == nil {
		return nil
	}
	attrs := make([]core.KeyValue, len(span.Resource.Labels))
	i := 0
	for k, v := range span.Resource.Labels {
		attrs[i] = key.String(k, v)
		i++
	}
	return resource.New(attrs...)
}

// OCProtoSpanToOTelSpanData converts an OC Span to an OTel SpanData.
func OCProtoSpanToOTelSpanData(span *tracepb.Span) (*trace.SpanData, error) {
	if span == nil {
		return nil, errors.New("expected a non-nil span")
	}

	spanData := &trace.SpanData{
		SpanContext: spanContext(span.GetTraceId(), span.GetSpanId()),
	}

	copy(spanData.ParentSpanID[:], span.GetParentSpanId()[:])
	spanData.Name = getSpanName(span)
	spanData.SpanKind = oTelSpanKind(span.GetKind())
	spanData.ChildSpanCount = int(span.GetChildSpanCount().GetValue())
	spanData.Links = createSpanLinks(span.GetLinks())
	spanData.Attributes = createOTelAttributes(span.GetAttributes())
	spanData.MessageEvents = createOTelEvents(span.GetTimeEvents())
	spanData.StartTime = timestampToTime(span.GetStartTime())
	spanData.EndTime = timestampToTime(span.GetEndTime())
	spanData.DroppedLinkCount = getDroppedLinkCount(span.GetLinks())
	spanData.ChildSpanCount = getChildSpanCount(span)
	spanData.Resource = spanResource(span)

	return spanData, nil
}
