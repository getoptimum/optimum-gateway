package consensus

import (
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

var (
	validSnappy   = [4]byte{1, 0, 0, 0}
	invalidSnappy = [4]byte{}
)

func MsgID(topic string, msgData []byte) string {
	decodedData, err := utils.DecodeSnappy(msgData, utils.MaxGossipPayloadSize)
	if err != nil {
		return hashData(invalidSnappy, topic, msgData)
	}
	return hashData(validSnappy, topic, decodedData)
}

func hashData(snappySign [4]byte, topic string, data []byte) string {
	topicLen := len(topic)
	topicLenBytes := utils.Uint64ToBytes(uint64(topicLen))

	totalLength := len(snappySign) + len(topicLenBytes) + len(topic) + len(data)
	result := make([]byte, 0, totalLength)
	result = append(result, snappySign[:]...)
	result = append(result, topicLenBytes...)
	result = append(result, topic...)
	result = append(result, data...)
	h := commonhash.SHA256String(result)
	return utils.UnsafeCastToString(h[:20])
}
