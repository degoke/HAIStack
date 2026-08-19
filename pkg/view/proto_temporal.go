package view

import (
	"fmt"
	"time"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	"google.golang.org/protobuf/proto"
)

const (
	layoutYear       = "2006"
	layoutMonth      = "2006-01"
	layoutDay        = "2006-01-02"
	layoutSeconds    = "2006-01-02T15:04:05-07:00"
	layoutSecondsUTC = "2006-01-02T15:04:05Z"
	layoutMillis     = "2006-01-02T15:04:05.000-07:00"
	layoutMillisUTC  = "2006-01-02T15:04:05.000Z"
	layoutMicros     = "2006-01-02T15:04:05.000000-07:00"
	layoutMicrosUTC  = "2006-01-02T15:04:05.000000Z"
	layoutTimeSecond = "15:04:05"
	layoutTimeMilli  = "15:04:05.000"
	layoutTimeMicro  = "15:04:05.000000"
)

func protoTemporalToString(msg proto.Message) (string, bool) {
	switch v := msg.(type) {
	case *dtpb.Date:
		s, err := formatDate(v)
		return s, err == nil
	case *dtpb.DateTime:
		s, err := formatDateTime(v)
		return s, err == nil
	case *dtpb.Time:
		s, err := formatTime(v)
		return s, err == nil
	case *dtpb.Instant:
		s, err := formatInstant(v)
		return s, err == nil
	default:
		return "", false
	}
}

func formatDate(d *dtpb.Date) (string, error) {
	if d == nil {
		return "", fmt.Errorf("nil date")
	}
	ts, err := timeFromValueUs(d.GetValueUs(), d.GetTimezone())
	if err != nil {
		return "", err
	}
	switch d.GetPrecision() {
	case dtpb.Date_YEAR:
		return ts.Format(layoutYear), nil
	case dtpb.Date_MONTH:
		return ts.Format(layoutMonth), nil
	case dtpb.Date_DAY:
		return ts.Format(layoutDay), nil
	default:
		return ts.Format(layoutDay), nil
	}
}

func formatDateTime(dt *dtpb.DateTime) (string, error) {
	if dt == nil {
		return "", fmt.Errorf("nil dateTime")
	}
	ts, err := timeFromValueUs(dt.GetValueUs(), dt.GetTimezone())
	if err != nil {
		return "", err
	}
	tz := dt.GetTimezone()
	switch dt.GetPrecision() {
	case dtpb.DateTime_YEAR:
		return ts.Format(layoutYear), nil
	case dtpb.DateTime_MONTH:
		return ts.Format(layoutMonth), nil
	case dtpb.DateTime_DAY:
		return ts.Format(layoutDay), nil
	case dtpb.DateTime_SECOND:
		if tz == "Z" {
			return ts.Format(layoutSecondsUTC), nil
		}
		return ts.Format(layoutSeconds), nil
	case dtpb.DateTime_MILLISECOND:
		if tz == "Z" {
			return ts.Format(layoutMillisUTC), nil
		}
		return ts.Format(layoutMillis), nil
	case dtpb.DateTime_MICROSECOND:
		if tz == "Z" {
			return ts.Format(layoutMicrosUTC), nil
		}
		return ts.Format(layoutMicros), nil
	default:
		if tz == "Z" {
			return ts.Format(layoutSecondsUTC), nil
		}
		return ts.Format(layoutSeconds), nil
	}
}

func formatTime(t *dtpb.Time) (string, error) {
	if t == nil {
		return "", fmt.Errorf("nil time")
	}
	ts := time.Unix(0, t.GetValueUs()*1000).UTC()
	switch t.GetPrecision() {
	case dtpb.Time_MILLISECOND:
		return ts.Format(layoutTimeMilli), nil
	case dtpb.Time_MICROSECOND:
		return ts.Format(layoutTimeMicro), nil
	default:
		return ts.Format(layoutTimeSecond), nil
	}
}

func formatInstant(in *dtpb.Instant) (string, error) {
	if in == nil {
		return "", fmt.Errorf("nil instant")
	}
	ts, err := timeFromValueUs(in.GetValueUs(), in.GetTimezone())
	if err != nil {
		return "", err
	}
	tz := in.GetTimezone()
	switch in.GetPrecision() {
	case dtpb.Instant_MILLISECOND:
		if tz == "Z" {
			return ts.Format(layoutMillisUTC), nil
		}
		return ts.Format(layoutMillis), nil
	case dtpb.Instant_MICROSECOND:
		if tz == "Z" {
			return ts.Format(layoutMicrosUTC), nil
		}
		return ts.Format(layoutMicros), nil
	default:
		if tz == "Z" {
			return ts.Format(layoutSecondsUTC), nil
		}
		return ts.Format(layoutSeconds), nil
	}
}

func timeFromValueUs(us int64, tz string) (time.Time, error) {
	if tz == "" || tz == "Z" {
		return time.Unix(us/1e6, (us%1e6)*1000).UTC(), nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Unix(us/1e6, (us%1e6)*1000).UTC(), nil
	}
	return time.Unix(us/1e6, (us%1e6)*1000).In(loc), nil
}
