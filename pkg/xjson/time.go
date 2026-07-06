package xjson

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type FlexTime int64

func (ft *FlexTime) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", s)
		}
		if err == nil {
			*ft = FlexTime(t.UnixMilli())
			return nil
		}
		if val, err := strconv.ParseInt(s, 10, 64); err == nil {
			*ft = FlexTime(val)
			return nil
		}
		if val, err := strconv.ParseFloat(s, 64); err == nil {
			*ft = FlexTime(int64(val))
			return nil
		}
	}
	var i int64
	if err := json.Unmarshal(b, &i); err == nil {
		*ft = FlexTime(i)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*ft = FlexTime(int64(f))
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into flexTime", string(b))
}
