package reversion

import (
	"time"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
)

const (
	FlowIDFundingReversion = "FUNDING_REVERSION"
)

const (
	TopicReversionCandidate         = "funding.reversion.candidate"
	TopicReversionArmMarketReady    = "funding.reversion.arm_market_ready"
	TopicReversionArmPlanCalculated = "funding.reversion.arm_plan_calculated"
	TopicReversionSafetyChecked     = "funding.reversion.safety_checked"
	TopicReversionArmed             = "funding.reversion.armed"
	TopicReversionWaitComplete      = "funding.reversion.wait_complete"
	TopicReversionConfirmed         = "funding.reversion.confirmed"
	TopicReversionMarginModeReady   = "funding.reversion.margin_mode_ready"
	TopicReversionFireTimingReady   = "funding.reversion.fire_timing_ready"
	TopicReversionFirePlanChecked   = "funding.reversion.fire_plan_checked"
	TopicReversionAbort             = "funding.reversion.abort"
)

type ReversionEvent interface {
	GetFlow() string
	GetReqID() string
	GetSymbol() string
	GetExchange() string
	GetOrderID() string
	GetExternalID() string
}

type BaseReversionEvent struct {
	Flow          string      `json:"flow,omitempty"`
	ReqID         string      `json:"req_id,omitempty"`
	Symbol        string      `json:"symbol"`
	Exchange      string      `json:"exchange,omitempty"`
	OrderID       string      `json:"order_id,omitempty"`
	ExternalID    string      `json:"external_id,omitempty"`
	Timestamp     time.Time   `json:"timestamp"`
	EventID       string      `json:"event_id,omitempty"`
	Seq           int64       `json:"seq,omitempty"`
	Topic         string      `json:"topic,omitempty"`
	PreviousTopic string      `json:"previous_topic,omitempty"`
	SettleTime    time.Time   `json:"settle_time"`
	Side          shared.Side `json:"side,omitempty"`
	FundingRate   float64     `json:"funding_rate,omitempty"`
	Vol24hUSDT    float64     `json:"vol_24h_usdt,omitempty"`
	ContractSize  float64     `json:"contract_size,omitempty"`
}

func (b BaseReversionEvent) GetFlow() string       { return b.Flow }
func (b BaseReversionEvent) GetReqID() string      { return b.ReqID }
func (b BaseReversionEvent) GetSymbol() string     { return b.Symbol }
func (b BaseReversionEvent) GetExchange() string   { return b.Exchange }
func (b BaseReversionEvent) GetOrderID() string    { return b.OrderID }
func (b BaseReversionEvent) GetExternalID() string { return b.ExternalID }

func (b BaseReversionEvent) DeduplicateKey() string {
	if b.ExternalID == "" || b.Topic == "" {
		return ""
	}
	return b.ExternalID + b.Topic
}

type CandidateFoundEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

type ArmMarketReadyEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	MaxWaitMs int64                   `json:"max_wait_ms"`
}

type ArmPlanCalculatedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
	IOCPrice  float64                 `json:"ioc_price"`
	RefPrice  float64                 `json:"ref_price"`
}

type SafetyCheckedEvent struct {
	BaseReversionEvent
	Candidate      fundingdomain.Candidate `json:"candidate"`
	IOCPrice       float64                 `json:"ioc_price"`
	RefPrice       float64                 `json:"ref_price"`
	AdjustedVolume float64                 `json:"adjusted_volume"`
	Passed         bool                    `json:"passed"`
	RejectReason   string                  `json:"reject_reason,omitempty"`
}

type ArmedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

type WaitCompleteEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

type ConfirmedEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

type MarginModeReadyEvent struct {
	BaseReversionEvent
	Candidate fundingdomain.Candidate `json:"candidate"`
}

type FireTimingReadyEvent struct {
	BaseReversionEvent
	Candidate        fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs     int64                   `json:"latency_rtt_ms"`
	FireOffsetMs     int64                   `json:"fire_offset_ms"`
	SnapshotOffsetMs int64                   `json:"snapshot_offset_ms"`
}

type FirePlanCheckedEvent struct {
	BaseReversionEvent
	Candidate      fundingdomain.Candidate `json:"candidate"`
	LatencyRTTMs   int64                   `json:"latency_rtt_ms"`
	FireOffsetMs   int64                   `json:"fire_offset_ms"`
	IOCPrice       float64                 `json:"ioc_price"`
	RefPrice       float64                 `json:"ref_price"`
	AdjustedVolume float64                 `json:"adjusted_volume"`
	Passed         bool                    `json:"passed"`
	RejectReason   string                  `json:"reject_reason,omitempty"`
}

type AbortEvent struct {
	BaseReversionEvent
	Reason string `json:"reason"`
}
