// Package otlp reads the metrics that a collector sends in the JSON form of the
// OpenTelemetry protocol and returns them as flat points. It keeps the name, the value
// and the time of a data point, and the attributes of its resource. It drops the rest.
//
// The package starts no program and reads no file.
package otlp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Point is one data point of a gauge or of a sum.
type Point struct {
	// Name is the name of the metric.
	Name string
	// Value is the integer or the floating-point value of the data point.
	Value float64
	// Time is the time of the data point on the clock of the sender.
	Time time.Time
	// Attributes are the attributes of the resource that have a text value. The points of
	// one resource share the map: a reader must not change it.
	Attributes map[string]string
}

type request struct {
	ResourceMetrics *[]resourceMetrics `json:"resourceMetrics"`
}

type resourceMetrics struct {
	Resource struct {
		Attributes []attribute `json:"attributes"`
	} `json:"resource"`
	ScopeMetrics []struct {
		Metrics []metric `json:"metrics"`
	} `json:"scopeMetrics"`
}

type attribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue *string `json:"stringValue"`
	} `json:"value"`
}

type metric struct {
	Name  string  `json:"name"`
	Gauge *series `json:"gauge"`
	Sum   *series `json:"sum"`
}

type series struct {
	DataPoints []dataPoint `json:"dataPoints"`
}

type dataPoint struct {
	TimeUnixNano json.RawMessage `json:"timeUnixNano"`
	AsInt        json.RawMessage `json:"asInt"`
	AsDouble     *float64        `json:"asDouble"`
}

// ParseMetrics decodes the body of an OTLP request for metrics in the JSON form.
//
// A metric that is not a gauge or a sum gives no point, and an attribute whose value is
// not text is left out. The errors name the position of the faulty entry and never
// repeat text of the body.
//
// Args:
//
//	body: the JSON object with the list resourceMetrics.
//
// Returns:
//
//	One point for each data point of each gauge and each sum, in the order of the body,
//	or an error when the body does not have the form of such a request.
func ParseMetrics(body []byte) ([]Point, error) {
	var decoded request
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("the body is not an OTLP request for metrics in JSON: %s",
			shape(err))
	}
	if decoder.More() {
		return nil, errors.New("the body has data after the JSON object")
	}
	if decoded.ResourceMetrics == nil {
		return nil, errors.New("the body has no list resourceMetrics")
	}
	points := []Point{}
	for r, resource := range *decoded.ResourceMetrics {
		found, err := resourcePoints(fmt.Sprintf("resourceMetrics[%d]", r), resource)
		if err != nil {
			return nil, err
		}
		points = append(points, found...)
	}
	return points, nil
}

func resourcePoints(where string, resource resourceMetrics) ([]Point, error) {
	attributes := map[string]string{}
	for _, attribute := range resource.Resource.Attributes {
		if attribute.Value.StringValue != nil {
			attributes[attribute.Key] = *attribute.Value.StringValue
		}
	}
	points := []Point{}
	for s, scope := range resource.ScopeMetrics {
		for m, metric := range scope.Metrics {
			at := fmt.Sprintf("%s.scopeMetrics[%d].metrics[%d]", where, s, m)
			found, err := metricPoints(at, metric, attributes)
			if err != nil {
				return nil, err
			}
			points = append(points, found...)
		}
	}
	return points, nil
}

// shape describes a decoding error with no text of the body: the error of a field with
// the wrong type names the field and its Go type only, and each other error is a syntax
// error at an offset.
func shape(err error) string {
	var wrongType *json.UnmarshalTypeError
	if errors.As(err, &wrongType) && wrongType.Field == "" {
		return "the body has the JSON type " + wrongType.Value
	}
	if errors.As(err, &wrongType) {
		return fmt.Sprintf("the field %s has the JSON type %s", wrongType.Field,
			wrongType.Value)
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("a syntax error at byte %d", syntax.Offset)
	}
	return "the JSON ends too early"
}

func metricPoints(where string, m metric, attributes map[string]string) ([]Point, error) {
	if m.Name == "" {
		return nil, fmt.Errorf("%s: the metric has no name", where)
	}
	points := []Point{}
	for _, kind := range []*series{m.Gauge, m.Sum} {
		if kind == nil {
			continue
		}
		for d, data := range kind.DataPoints {
			at := fmt.Sprintf("%s.dataPoints[%d]", where, d)
			value, err := pointValue(at, data)
			if err != nil {
				return nil, err
			}
			nanos, err := digits(at, "timeUnixNano", data.TimeUnixNano)
			if err != nil {
				return nil, err
			}
			points = append(points, Point{Name: m.Name, Value: value,
				Time: time.Unix(0, nanos).UTC(), Attributes: attributes})
		}
	}
	return points, nil
}

func pointValue(where string, data dataPoint) (float64, error) {
	if data.AsDouble != nil {
		return *data.AsDouble, nil
	}
	if data.AsInt == nil {
		return 0, fmt.Errorf("%s: the data point has no asInt and no asDouble", where)
	}
	value, err := digits(where, "asInt", data.AsInt)
	return float64(value), err
}

// digits reads an integer of 64 bits, which the JSON form writes as a string of digits.
// It accepts a JSON number too.
func digits(where, field string, raw json.RawMessage) (int64, error) {
	text := string(raw)
	if len(text) == 0 || text == "null" {
		return 0, fmt.Errorf("%s: the data point has no %s", where, field)
	}
	if text[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, fmt.Errorf("%s: %s is not a string of digits", where, field)
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %s is not a string of digits", where, field)
	}
	return value, nil
}
