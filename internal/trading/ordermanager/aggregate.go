package ordermanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/formatutil"
)

// OrderLifecycleState represents FSM state in order execution lifecycle.
type OrderLifecycleState string

const (
	StateInit               OrderLifecycleState = "INIT"
	StatePreFlightDone      OrderLifecycleState = "PREFLIGHT_DONE"
	StateFireWindow         OrderLifecycleState = "FIRE_WINDOW"
	StatePositionWatchReady OrderLifecycleState = "POSITION_WATCH_READY"
	StateSubmitted          OrderLifecycleState = "SUBMITTED"
	StateResting            OrderLifecycleState = "RESTING"
	StateTPSLDispatched     OrderLifecycleState = "TPSL_DISPATCHED"
	StateTimeoutScheduled   OrderLifecycleState = "TIMEOUT_SCHEDULED"
	StateFilled             OrderLifecycleState = "FILLED"
	StatePositionClosed     OrderLifecycleState = "POSITION_CLOSED"
	StateOutcomeResolved    OrderLifecycleState = "OUTCOME_RESOLVED"
	StateTimeoutChecked     OrderLifecycleState = "TIMEOUT_CHECKED"
	StateBailout            OrderLifecycleState = "BAILOUT"
	StateAborted            OrderLifecycleState = "ABORTED"
	StateCompleted          OrderLifecycleState = "COMPLETED"
)

// OrderExecutionAggregate is the pure Event-Sourced aggregate managing order lifecycle states.
type OrderExecutionAggregate struct {
	mu                sync.RWMutex
	reqID             string
	state             OrderLifecycleState
	version           int64
	uncommittedEvents []OrderEvent
}

// NewOrderExecutionAggregate creates an uninitialized aggregate.
func NewOrderExecutionAggregate(reqID string) *OrderExecutionAggregate {
	return &OrderExecutionAggregate{
		reqID: reqID,
		state: StateInit,
	}
}

func (a *OrderExecutionAggregate) ReqID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.reqID != "" {
		return a.reqID
	}
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetReqID() != "" {
			return e.GetReqID()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) RefID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil {
			if intent, ok := e.(OrderIntentEvent); ok && intent.RefID != "" {
				return intent.RefID
			}
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) ClientOrderID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetClientOrderID() != "" {
			return e.GetClientOrderID()
		}
	}
	if a.reqID != "" {
		return a.reqID
	}
	return ""
}

func (a *OrderExecutionAggregate) Symbol() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetSymbol() != "" {
			return e.GetSymbol()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) Exchange() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetExchange() != "" {
			return e.GetExchange()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) MarketType() MarketType {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetMarketType() != "" {
			return e.GetMarketType()
		}
	}
	return MarketTypeFuture
}

func (a *OrderExecutionAggregate) StrategyType() StrategyType {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetStrategyType() != "" {
			return e.GetStrategyType()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) Side() shared.Side {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		if e, ok := evt.(OrderIntentEvent); ok {
			return e.Side
		}
	}
	return 0
}

func (a *OrderExecutionAggregate) ContractSize() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderIntentEvent:
			if e.ContractSize > 0 {
				return e.ContractSize
			}
		case OrderSubmittedEvent:
			if e.ContractSize > 0 {
				return e.ContractSize
			}
		case OrderCompletedEvent:
			if e.ContractSize > 0 {
				return e.ContractSize
			}
		}
	}
	return 1.0
}

func (a *OrderExecutionAggregate) FillVolContract() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderFilledEvent:
			if e.FillVolContract > 0 {
				return e.FillVolContract
			}
		case OrderPositionClosedEvent:
			if e.CloseVolContract > 0 {
				return e.CloseVolContract
			}
		case OrderOutcomeResolvedEvent:
			if e.FilledVol > 0 {
				return e.FilledVol
			}
		}
	}
	return 0
}

func (a *OrderExecutionAggregate) FillVolCoin() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderFilledEvent:
			if e.FillVolCoin > 0 {
				return e.FillVolCoin
			}
		case OrderPositionClosedEvent:
			if e.CloseVolCoin > 0 {
				return e.CloseVolCoin
			}
		}
	}
	return 0
}

func (a *OrderExecutionAggregate) State() OrderLifecycleState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *OrderExecutionAggregate) Version() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.version
}

func (a *OrderExecutionAggregate) UncommittedEvents() []OrderEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	events := make([]OrderEvent, len(a.uncommittedEvents))
	copy(events, a.uncommittedEvents)
	return events
}

func (a *OrderExecutionAggregate) ClearUncommittedEvents() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uncommittedEvents = nil
}

// HasSubmitted returns true if OrderSubmittedEvent has been recorded for this aggregate or state is StateSubmitted or higher.
func (a *OrderExecutionAggregate) HasSubmitted() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if stateRank(a.state) >= stateRank(StateSubmitted) {
		return true
	}
	for _, evt := range a.uncommittedEvents {
		if _, ok := evt.(OrderSubmittedEvent); ok {
			return true
		}
	}
	return false
}

// HasFilled returns true if OrderFilledEvent has been recorded for this aggregate or state is StateFilled or higher.
func (a *OrderExecutionAggregate) HasFilled() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if stateRank(a.state) >= stateRank(StateFilled) {
		return true
	}
	for _, evt := range a.uncommittedEvents {
		if _, ok := evt.(OrderFilledEvent); ok {
			return true
		}
	}
	return false
}

func stateRank(s OrderLifecycleState) int {
	switch s {
	case StateInit:
		return 1
	case StatePreFlightDone:
		return 2
	case StateFireWindow:
		return 3
	case StatePositionWatchReady:
		return 4
	case StateSubmitted:
		return 5
	case StateResting:
		return 6
	case StateTPSLDispatched, StateTimeoutScheduled:
		return 7
	case StateFilled, StateOutcomeResolved:
		return 8
	case StateTimeoutChecked:
		return 9
	case StateBailout, StatePositionClosed:
		return 10
	case StateCompleted, StateAborted:
		return 11
	default:
		return 0
	}
}

//nolint:cyclop // FSM event state resolution switch handles all micro-event types
func resolveEventNextState(evt OrderEvent) OrderLifecycleState {
	switch e := evt.(type) {
	case OrderIntentEvent:
		return StateInit
	case OrderPreFlightCompletedEvent:
		return StatePreFlightDone
	case OrderFireWindowReachedEvent:
		return StateFireWindow
	case OrderPositionWatchReadyEvent:
		return StatePositionWatchReady
	case OrderSubmittedEvent:
		return StateSubmitted
	case OrderRestingEvent:
		return StateResting
	case OrderTPSLDispatchedEvent:
		return StateTPSLDispatched
	case OrderTimeoutScheduledEvent:
		return StateTimeoutScheduled
	case OrderFilledEvent:
		return StateFilled
	case OrderPositionClosedEvent:
		return StatePositionClosed
	case OrderOutcomeResolvedEvent:
		if e.Outcome == OutcomeResting {
			return StateResting
		}
		return StateOutcomeResolved
	case OrderTimeoutPositionCheckedEvent:
		return StateTimeoutChecked
	case OrderBailoutExecutedEvent:
		return StateBailout
	case OrderCompletedEvent:
		return StateCompleted
	case OrderAbortedEvent:
		return StateAborted
	default:
		return ""
	}
}

func (a *OrderExecutionAggregate) PositionMode() shared.PositionMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderIntentEvent:
			if e.PositionMode != 0 {
				return e.PositionMode
			}
		case OrderSubmittedEvent:
			if e.PositionMode != 0 {
				return e.PositionMode
			}
		}
	}
	return shared.PositionModeHedge
}

func (a *OrderExecutionAggregate) Leverage() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderIntentEvent:
			if e.Leverage > 0 {
				return e.Leverage
			}
		case OrderPreFlightCompletedEvent:
			if e.AdjustedLeverage > 0 {
				return e.AdjustedLeverage
			}
		case OrderSubmittedEvent:
			if e.Leverage > 0 {
				return e.Leverage
			}
		}
	}
	return 1
}

func (a *OrderExecutionAggregate) OrderType() OrderType {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderIntentEvent:
			if e.OrderType != "" {
				return e.OrderType
			}
		case OrderSubmittedEvent:
			if e.OrderType != "" {
				return e.OrderType
			}
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) TimeoutDuration() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, evt := range a.uncommittedEvents {
		switch e := evt.(type) {
		case OrderIntentEvent:
			if e.TimeoutDuration > 0 {
				return e.TimeoutDuration
			}
		case OrderSubmittedEvent:
			if e.TimeoutDuration > 0 {
				return e.TimeoutDuration
			}
		case OrderTimeoutScheduledEvent:
			if e.Duration > 0 {
				return e.Duration
			}
		}
	}
	return 0
}

// Apply performs pure state transitions based on incoming micro-events.
func (a *OrderExecutionAggregate) Apply(evt OrderEvent) error {
	if evt == nil {
		return fmt.Errorf("cannot apply nil event")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyLocked(evt)
}

func (a *OrderExecutionAggregate) applyLocked(evt OrderEvent) error {
	nextState := resolveEventNextState(evt)
	if stateRank(nextState) >= stateRank(a.state) {
		a.state = nextState
	}

	a.version++
	return nil
}

// Record appends an uncommitted event after applying state transition.
func (a *OrderExecutionAggregate) Record(evt OrderEvent) error {
	if evt == nil {
		return fmt.Errorf("cannot record nil event")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.applyLocked(evt); err != nil {
		return err
	}
	a.uncommittedEvents = append(a.uncommittedEvents, evt)
	return nil
}

// BuildTradeRecord loops through recorded events array to get final trade record data to save to DB.
func (a *OrderExecutionAggregate) BuildTradeRecord() OrderTradeRecordEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var record OrderTradeRecordEvent

	for _, evt := range a.uncommittedEvents {
		if evt == nil {
			continue
		}
		record.applyEventBase(evt)
		record.applyEventPayload(evt)
	}

	record.PreTopic = TopicOrderCompleted
	record.NextTopic = TopicOrderTradeRecord

	return record
}

func (r *OrderTradeRecordEvent) applyEventBase(evt OrderEvent) {
	if reqID := evt.GetReqID(); reqID != "" {
		r.ReqID = reqID
	}
	if clientOID := evt.GetClientOrderID(); clientOID != "" {
		r.ClientOrderID = clientOID
		r.BaseExecutionEvent.ClientOrderID = clientOID
	}
	if sym := evt.GetSymbol(); sym != "" {
		r.Symbol = sym
		r.NormalizedSymbol = formatutil.GetNormalizedSymbol(sym)
	}
	if ex := evt.GetExchange(); ex != "" {
		r.Exchange = ex
	}
	if mt := evt.GetMarketType(); mt != "" {
		r.MarketType = string(mt)
		r.BaseExecutionEvent.MarketType = mt
	}
	if st := evt.GetStrategyType(); st != "" {
		r.StrategyType = st
	}

	if pre := evt.GetPreTopic(); pre != "" {
		r.PreTopic = pre
	}
	if next := evt.GetNextTopic(); next != "" {
		r.NextTopic = next
	}
}

func (r *OrderTradeRecordEvent) applyEventPayload(evt OrderEvent) {
	switch e := evt.(type) {
	case OrderIntentEvent:
		r.applyIntentPayload(e)
	case OrderPreFlightCompletedEvent:
		if e.AdjustedLeverage > 0 {
			r.Leverage = e.AdjustedLeverage
		}
	case OrderFireWindowReachedEvent:
		if !e.FireWindowReachedAt.IsZero() {
			fireAt := e.FireWindowReachedAt
			r.FireAt = &fireAt
		}
	case OrderSubmittedEvent:
		if e.OrderID != "" {
			r.ExchangeOrderID = e.OrderID
		}
	case OrderFilledEvent:
		r.applyFilledPayload(e)
	case OrderPositionClosedEvent:
		r.applyClosedPayload(e)
	case OrderOutcomeResolvedEvent:
		r.applyOutcomePayload(e)
	case OrderBailoutExecutedEvent:
		r.applyBailoutPayload(e)
	case OrderCompletedEvent:
		r.applyCompletedPayload(e)
	case OrderAbortedEvent:
		r.applyAbortedPayload(e)
	}
}

func (r *OrderTradeRecordEvent) applyFilledPayload(e OrderFilledEvent) {
	if e.FillPrice > 0 {
		r.EntryPrice = e.FillPrice
	}
	if e.FillVolContract > 0 {
		r.FillVolContract = e.FillVolContract
		if r.CloseVolContract == 0 {
			r.CloseVolContract = e.FillVolContract
		}
	}
	if e.FillVolCoin > 0 {
		r.FillVolCoin = e.FillVolCoin
		if r.CloseVolCoin == 0 {
			r.CloseVolCoin = e.FillVolCoin
		}
	}
	if e.SlippagePct > 0 {
		r.ActualSlippage = e.SlippagePct
	}
}

func (r *OrderTradeRecordEvent) applyClosedPayload(e OrderPositionClosedEvent) {
	r.EntryPrice = e.EntryPrice
	r.ExitPrice = e.ClosePrice
	if e.CloseVolContract > 0 {
		r.CloseVolContract = e.CloseVolContract
	}
	if e.CloseVolCoin > 0 {
		r.CloseVolCoin = e.CloseVolCoin
	}
	r.GrossPnL = e.GrossProfit
	r.NetPnL = e.NetProfit
	r.PnLPct = e.PnLPct
	r.Fee = e.Fee
	r.FundingFee = e.FundingFee
	r.HoldDurationMs = e.HoldDurationMs
	r.Reason = e.Reason
	if e.VolumeUSDT > 0 {
		r.NotionalUSD = e.VolumeUSDT
	}
}

func (r *OrderTradeRecordEvent) applyOutcomePayload(e OrderOutcomeResolvedEvent) {
	if r.Outcome == "" {
		r.Outcome = string(e.Outcome)
	}
	if r.Reason == "" {
		r.Reason = e.Reason
	}
	if e.FilledVol > 0 && r.FillVolContract == 0 && r.FillVolCoin == 0 {
		r.FillVolContract = e.FilledVol
	}
}

func (r *OrderTradeRecordEvent) applyIntentPayload(e OrderIntentEvent) {
	r.Side = e.Side.String()
	r.OrderType = string(e.OrderType)
	r.OrderVol = e.Volume
	if e.ContractSize > 0 {
		r.ContractSize = e.ContractSize
	}
	if e.Leverage > 0 {
		r.Leverage = e.Leverage
		cs := e.ContractSize
		if cs <= 0 {
			cs = 1.0
		}
		if e.Price > 0 && e.Volume > 0 {
			r.MarginUSDT = (e.Price * e.Volume * cs) / float64(e.Leverage)
		}
	}
	if e.MaxLatency > 0 {
		r.LatencyRTTMs = e.MaxLatency.Milliseconds()
	}
	switch val := e.Extra["latency_rtt_ms"].(type) {
	case int64:
		r.LatencyRTTMs = val
	case float64:
		r.LatencyRTTMs = int64(val)
	}
	if !e.FireTime.IsZero() {
		fireAt := e.FireTime
		r.FireAt = &fireAt
	}
	if e.SettleTime != nil {
		r.SettleTime = e.SettleTime
	}
	if len(e.Extra) > 0 {
		r.Extra = e.Extra
	}
}

func (r *OrderTradeRecordEvent) applyBailoutPayload(e OrderBailoutExecutedEvent) {
	r.ForceCloseAttempted = true
	if e.ExitPrice > 0 || e.CloseRetryCount < 3 {
		r.ForceCloseSucceeded = true
	}
	r.ExitPrice = e.ExitPrice
	r.CloseRetryCount = e.CloseRetryCount
	r.Reason = e.Reason
	r.Outcome = "bailout"
	r.Status = StatusAborted
}

func (r *OrderTradeRecordEvent) applyAbortedPayload(e OrderAbortedEvent) {
	if r.Outcome == "" {
		r.Outcome = string(OutcomeAborted)
	}
	r.Status = StatusAborted
	if e.Reason != "" {
		r.Reason = e.Reason
	}
	if e.Error != "" {
		if r.Reason != "" && r.Reason != e.Error {
			r.Reason = fmt.Sprintf("%s: %s", r.Reason, e.Error)
		} else {
			r.Reason = e.Error
		}
	}
}

func (r *OrderTradeRecordEvent) applyCompletedPricingAndVolume(e OrderCompletedEvent) {
	if e.OrderID != "" {
		r.ExchangeOrderID = e.OrderID
	}
	if e.Side.String() != "" {
		r.Side = e.Side.String()
	}
	if e.EntryPrice > 0 {
		r.EntryPrice = e.EntryPrice
	}
	if e.ExitPrice > 0 {
		r.ExitPrice = e.ExitPrice
	}
	if e.CloseVolContract > 0 {
		r.CloseVolContract = e.CloseVolContract
	}
	if e.CloseVolCoin > 0 {
		r.CloseVolCoin = e.CloseVolCoin
	}
	if e.ContractSize > 0 {
		r.ContractSize = e.ContractSize
	}

	cs := r.ContractSize
	if cs <= 0 {
		cs = 1.0
	}
	if e.VolumeUSDT > 0 {
		r.NotionalUSD = e.VolumeUSDT
	} else if r.ExitPrice > 0 {
		if r.CloseVolCoin > 0 {
			r.NotionalUSD = r.CloseVolCoin * r.ExitPrice
		} else if r.CloseVolContract > 0 {
			r.NotionalUSD = r.CloseVolContract * r.ExitPrice * cs
		}
	}
}

func (r *OrderTradeRecordEvent) applyCompletedPnLAndFees(e OrderCompletedEvent) {
	if e.GrossProfit != 0 {
		r.GrossPnL = e.GrossProfit
	}
	if e.NetProfit != 0 {
		r.NetPnL = e.NetProfit
	}
	if e.PnLPct != 0 {
		r.PnLPct = e.PnLPct
	}
	if e.Fee != 0 {
		r.Fee = e.Fee
	}
	if e.FundingFee != 0 {
		r.FundingFee = e.FundingFee
	}
	if e.CloseRetryCount > 0 {
		r.CloseRetryCount = e.CloseRetryCount
	}
	if e.Outcome != "" {
		r.Outcome = string(e.Outcome)
	}
	if e.Reason != "" {
		r.Reason = e.Reason
	}
	if e.HoldDurationMs > 0 {
		r.HoldDurationMs = e.HoldDurationMs
	}
}

func (r *OrderTradeRecordEvent) applyCompletedPayload(e OrderCompletedEvent) {
	r.applyCompletedPricingAndVolume(e)
	r.applyCompletedPnLAndFees(e)

	switch e.Outcome {
	case OutcomeFilled, OutcomePartialFilled, OrderOutcome("bailout"), OrderOutcome("completed"):
		r.Status = StatusCompleted
	case OutcomeAborted, OutcomeCanceledNoFill:
		r.Status = StatusAborted
	default:
		if r.Status == "" {
			r.Status = StatusCompleted
		}
	}
	r.RecordedAt = e.CompletedAt
	r.Timestamp = e.CompletedAt
	r.HoldDurationMs = e.HoldDurationMs
	if e.SettleTime != nil {
		r.SettleTime = e.SettleTime
	}
}

// Replay reconstructs aggregate state deterministically by replaying historical event stream.
func (a *OrderExecutionAggregate) Replay(events []OrderEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = StateInit
	a.version = 0
	a.uncommittedEvents = nil

	for _, evt := range events {
		if evt == nil {
			continue
		}
		if err := a.applyLocked(evt); err != nil {
			return fmt.Errorf("failed to replay event at version %d: %w", a.version+1, err)
		}
		a.uncommittedEvents = append(a.uncommittedEvents, evt)
	}
	return nil
}

// Handle executes domain context validation and returns proposed downstream events.
func (a *OrderExecutionAggregate) Handle(ctx context.Context, evt OrderEvent) ([]OrderEvent, error) {
	if err := a.Record(evt); err != nil {
		return nil, err
	}
	return []OrderEvent{evt}, nil
}
