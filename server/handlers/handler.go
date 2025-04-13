package handlers

import (
	"time"

	"server/responses"
)

func buildMetadata(sentAt, receivedAt string) responses.ResponseMetadata {
	return buildMetadataWithRespondedAt(sentAt, receivedAt, time.Now().Format(time.RFC3339))
}

func buildMetadataWithRespondedAt(sentAt, receivedAt, respondedAt string) responses.ResponseMetadata {
	return responses.ResponseMetadata{
		SentAt:      sentAt,
		ReceivedAt:  receivedAt,
		RespondedAt: respondedAt,
	}
}
