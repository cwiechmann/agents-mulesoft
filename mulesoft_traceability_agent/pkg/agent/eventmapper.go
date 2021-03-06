package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	apiV1 "github.com/Axway/agent-sdk/pkg/apic/apiserver/models/api/v1"
	"github.com/Axway/agents-mulesoft/mulesoft_traceability_agent/pkg/anypoint"

	"github.com/Axway/agent-sdk/pkg/agent"
	"github.com/Axway/agent-sdk/pkg/transaction"
	"github.com/Axway/agent-sdk/pkg/util/log"
)

// EventMapper -
type EventMapper struct {
}

func (m *EventMapper) processMapping(anypointAnalyticsEvent anypoint.AnalyticsEvent) ([]*transaction.LogEvent, error) {
	item, err := agent.GetAPICache().GetItem(anypointAnalyticsEvent.APIVersionID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		// API not in central
		return nil, nil
	}

	centralCfg := agent.GetCentralConfig()

	eventTime := anypointAnalyticsEvent.Timestamp.UnixNano() / 1000000
	txID := fmt.Sprintf("%s-%s", anypointAnalyticsEvent.APIVersionID, anypointAnalyticsEvent.MessageID)
	txEventID := anypointAnalyticsEvent.MessageID
	transInboundLogEventLeg, err := m.createTransactionEvent(eventTime, txID, anypointAnalyticsEvent, txEventID+"-leg0", "", "Inbound")
	if err != nil {
		return nil, err
	}

	transOutboundLogEventLeg, err := m.createTransactionEvent(eventTime, txID, anypointAnalyticsEvent, txEventID+"-leg1", txEventID+"-leg0", "Outbound")
	if err != nil {
		return nil, err
	}

	transSummaryLogEvent, err := m.createSummaryEvent(eventTime, txID, anypointAnalyticsEvent, centralCfg.GetTeamID())
	if err != nil {
		return nil, err
	}

	return []*transaction.LogEvent{
		transSummaryLogEvent,
		transInboundLogEventLeg,
		transOutboundLogEventLeg,
	}, nil
}

func (m *EventMapper) getTransactionEventStatus(code int) transaction.TxEventStatus {
	if code >= 400 {
		return transaction.TxEventStatusFail
	}
	return transaction.TxEventStatusFail
}

func (m *EventMapper) getTransactionSummaryStatus(statusCode int) transaction.TxSummaryStatus {
	transSummaryStatus := transaction.TxSummaryStatusUnknown
	if statusCode >= http.StatusOK && statusCode < http.StatusBadRequest {
		transSummaryStatus = transaction.TxSummaryStatusSuccess
	} else if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		transSummaryStatus = transaction.TxSummaryStatusFailure
	} else if statusCode >= http.StatusInternalServerError && statusCode < http.StatusNetworkAuthenticationRequired {
		transSummaryStatus = transaction.TxSummaryStatusException
	}
	return transSummaryStatus
}

func (m *EventMapper) buildHeaders(headers map[string]string) string {
	jsonHeader, err := json.Marshal(headers)
	if err != nil {
		log.Error(err.Error())
	}
	return string(jsonHeader)
}

func (m *EventMapper) createTransactionEvent(eventTime int64, txID string, txDetails anypoint.AnalyticsEvent, eventID, parentEventID, direction string) (*transaction.LogEvent, error) {
	//TODO - Slim pickings on header data
	req := map[string]string{"User-AgentName": txDetails.UserAgentName}
	res := map[string]string{"Request-Outcome": txDetails.RequestOutcome}
	httpProtocolDetails, err := transaction.NewHTTPProtocolBuilder().
		SetURI(txDetails.ResourcePath).
		SetMethod(txDetails.Verb).
		SetStatus(txDetails.StatusCode, http.StatusText(txDetails.StatusCode)).
		SetHost(txDetails.ClientIP).
		SetHeaders(m.buildHeaders(req), m.buildHeaders(res)).
		SetByteLength(txDetails.RequestSize, txDetails.ResponseSize).
		//SetRemoteAddress("", txDetails.DesHost, txDetails.DestPort).
		//SetLocalAddress(txDetails.SourceHost, txDetails.SourcePort).
		Build()
	if err != nil {
		return nil, err
	}

	return transaction.NewTransactionEventBuilder().
		SetTimestamp(eventTime).
		SetTransactionID(txID).
		SetID(eventID).
		SetParentID(parentEventID).
		SetSource(txDetails.ClientIP + ":0").
		SetDestination(txDetails.APIName). // TODO
		SetDirection(direction).
		SetStatus(m.getTransactionEventStatus(txDetails.StatusCode)).
		SetProtocolDetail(httpProtocolDetails).
		Build()
}

func (m *EventMapper) createSummaryEvent(eventTime int64, txID string, anypointAnalyticsEvent anypoint.AnalyticsEvent, teamID string) (*transaction.LogEvent, error) {
	statusCode := anypointAnalyticsEvent.StatusCode
	method := anypointAnalyticsEvent.Verb
	uri := anypointAnalyticsEvent.ResourcePath // TODO
	host := anypointAnalyticsEvent.ClientIP

	res, _ := agent.GetAPICache().Get(anypointAnalyticsEvent.APIVersionID)
	resInstance := res.(apiV1.ResourceInstance)

	return transaction.NewTransactionSummaryBuilder().
		SetTimestamp(eventTime).
		SetTransactionID(txID).
		SetStatus(m.getTransactionSummaryStatus(statusCode), strconv.Itoa(statusCode)).
		SetDuration(anypointAnalyticsEvent.ResponseTime).
		SetTeam(teamID).
		SetEntryPoint("http", method, uri, host).
		SetProxy(transaction.FormatProxyID(anypointAnalyticsEvent.APIVersionID), resInstance.ResourceMeta.Title, 1).
		Build()
}
