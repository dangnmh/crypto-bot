package ordermanager

import (
	"context"
	"fmt"

	shared "crypto-bot/internal/domain"
)

// OrderLifecycleState represents FSM state in order execution lifecycle.
type OrderLifecycleState string

const (
	StateInit            OrderLifecycleState = "INIT"
	StatePreFlightDone   OrderLifecycleState = "PREFLIGHT_DONE"
	StateFireWindow      OrderLifecycleState = "FIRE_WINDOW"
	StateSubmitted       OrderLifecycleState = "SUBMITTED"
	StateOutcomeResolved OrderLifecycleState = "OUTCOME_RESOLVED"
	StateBailout         OrderLifecycleState = "BAILOUT"
	StateCompleted       OrderLifecycleState = "COMPLETED"
)

// OrderExecutionAggregate is the pure Event-Sourced aggregate managing order lifecycle states.
type OrderExecutionAggregate struct {
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

func (a *OrderExecutionAggregate) ClientOrderID() string {
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetClientOrderID() != "" {
			return e.GetClientOrderID()
		}
	}
	return a.ReqID()
}

func (a *OrderExecutionAggregate) Symbol() string {
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetSymbol() != "" {
			return e.GetSymbol()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) Exchange() string {
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetExchange() != "" {
			return e.GetExchange()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) MarketType() MarketType {
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetMarketType() != "" {
			return e.GetMarketType()
		}
	}
	return MarketTypeFuture
}

func (a *OrderExecutionAggregate) StrategyType() StrategyType {
	for _, e := range a.uncommittedEvents {
		if e != nil && e.GetStrategyType() != "" {
			return e.GetStrategyType()
		}
	}
	return ""
}

func (a *OrderExecutionAggregate) Side() shared.Side {
	for _, evt := range a.uncommittedEvents {
		if e, ok := evt.(OrderIntentEvent); ok {
			return e.Side
		}
	}
	return 0
}

func (a *OrderExecutionAggregate) State() OrderLifecycleState      { return a.state }
func (a *OrderExecutionAggregate) Version() int64                  { return a.version }
func (a *OrderExecutionAggregate) UncommittedEvents() []OrderEvent { return a.uncommittedEvents }

func (a *OrderExecutionAggregate) ClearUncommittedEvents() {
	a.uncommittedEvents = nil
}

// Apply performs pure state transitions based on incoming micro-events.
func (a *OrderExecutionAggregate) Apply(evt OrderEvent) error {
	if evt == nil {
		return fmt.Errorf("cannot apply nil event")
	}

	switch evt.(type) {
	case OrderIntentEvent:
		a.state = StateInit

	case OrderPreFlightCompletedEvent:
		a.state = StatePreFlightDone

	case OrderFireWindowReachedEvent:
		a.state = StateFireWindow

	case OrderSubmittedEvent:
		a.state = StateSubmitted

	case OrderOutcomeResolvedEvent:
		a.state = StateOutcomeResolved

	case OrderBailoutExecutedEvent:
		a.state = StateBailout

	case OrderCompletedEvent:
		a.state = StateCompleted
	}

	a.version++
	return nil
}

// Record appends an uncommitted event after applying state transition.
func (a *OrderExecutionAggregate) Record(evt OrderEvent) error {
	if err := a.Apply(evt); err != nil {
		return err
	}
	a.uncommittedEvents = append(a.uncommittedEvents, evt)
	return nil
}

// BuildTradeRecord loops through recorded events array to get final trade record data to save to DB.
func (a *OrderExecutionAggregate) BuildTradeRecord() OrderTradeRecordEvent {
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
	r.SendNotify = evt.ShouldNotify()
}

func (r *OrderTradeRecordEvent) applyEventPayload(evt OrderEvent) {
	switch e := evt.(type) {
	case OrderIntentEvent:
		r.applyIntentPayload(e)
	case OrderPreFlightCompletedEvent:
		if e.AdjustedLeverage > 0 {
			r.Leverage = e.AdjustedLeverage
		}
	case OrderOutcomeResolvedEvent:
		if r.Outcome == "" {
			r.Outcome = string(e.Outcome)
		}
		if r.Reason == "" {
			r.Reason = e.Reason
		}
		if e.FilledVol > 0 {
			r.FilledVol = e.FilledVol
		}
	case OrderBailoutExecutedEvent:
		r.applyBailoutPayload(e)
	case OrderCompletedEvent:
		r.applyCompletedPayload(e)
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
	r.Status = "aborted"
}

func (r *OrderTradeRecordEvent) applyCompletedPayload(e OrderCompletedEvent) {
	r.EntryPrice = e.EntryPrice
	r.ExitPrice = e.ExitPrice
	if e.Volume > 0 {
		r.FilledVol = e.Volume
	}
	if e.ContractSize > 0 {
		r.ContractSize = e.ContractSize
	}

	cs := r.ContractSize
	if cs <= 0 {
		cs = 1.0
	}
	r.NotionalUSD = r.FilledVol * e.ExitPrice * cs

	r.GrossPnL = e.GrossProfit
	r.NetPnL = e.NetProfit
	r.PnLPct = e.PnLPct
	r.Fee = e.Fee
	r.FundingFee = e.FundingFee
	if e.CloseRetryCount > 0 {
		r.CloseRetryCount = e.CloseRetryCount
	}
	r.Outcome = e.Outcome
	r.Reason = e.Reason
	if r.Status == "" {
		r.Status = "completed"
	}
	r.RecordedAt = e.CompletedAt
	r.Timestamp = e.CompletedAt
}

// Replay reconstructs aggregate state deterministically by replaying historical event stream.
func (a *OrderExecutionAggregate) Replay(events []OrderEvent) error {
	a.state = StateInit
	a.version = 0
	a.uncommittedEvents = nil

	for _, evt := range events {
		if err := a.Record(evt); err != nil {
			return fmt.Errorf("failed to replay event at version %d: %w", a.version+1, err)
		}
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
