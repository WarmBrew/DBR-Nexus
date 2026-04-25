package protocol

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Envelope is the wire format for all WebSocket messages
type Envelope struct {
	ID        string          `json:"id"`
	Channel   string          `json:"channel"`
	Type      string          `json:"type"`
	Action    string          `json:"action"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"ts"`
	Error     *ProtocolError  `json:"error,omitempty"`
}

// ProtocolError represents an error in the protocol
type ProtocolError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewEnvelope creates a new envelope with the given parameters
func NewEnvelope(id, channel, msgType, action string, payload interface{}) (*Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &Envelope{
		ID:        id,
		Channel:   channel,
		Type:      msgType,
		Action:    action,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// NewResponse creates a response envelope for the given request
func NewResponse(req *Envelope, payload interface{}) (*Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &Envelope{
		ID:        req.ID,
		Channel:   req.Channel,
		Type:      TypeResponse,
		Action:    req.Action,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// NewErrorResponse creates an error response for the given request
func NewErrorResponse(req *Envelope, code int, message string) *Envelope {
	return &Envelope{
		ID:        req.ID,
		Channel:   req.Channel,
		Type:      TypeResponse,
		Action:    req.Action,
		Timestamp: time.Now().UnixMilli(),
		Error: &ProtocolError{
			Code:    code,
			Message: message,
		},
	}
}

// NewEvent creates an event envelope
func NewEvent(channel, action string, payload interface{}) (*Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return &Envelope{
		ID:        uuid.New().String(),
		Channel:   channel,
		Type:      TypeEvent,
		Action:    action,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// DecodePayload decodes the payload into the target
func (e *Envelope) DecodePayload(target interface{}) error {
	if e.Payload == nil {
		return nil
	}
	return json.Unmarshal(e.Payload, target)
}

// GenerateID generates a UUID v4
func GenerateID() string {
	return uuid.New().String()
}
