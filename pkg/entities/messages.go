package entities

const (
	ResponseCodeSuccess        = byte(0x00)
	ResponseCodeInvalidRequest = byte(0x01)
)

type CLMessage struct {
	MessageID    string `json:"message_id"`
	Topic        string `json:"topic"`
	Message      []byte `json:"message"`
	ReceivedFrom string `json:"received_from,omitempty"`
}
