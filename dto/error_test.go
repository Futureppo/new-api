package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGeneralErrorResponseDetailObjectMessage(t *testing.T) {
	var resp GeneralErrorResponse
	err := common.Unmarshal([]byte(`{"detail":{"error":"Content violates usage guidelines"}}`), &resp)
	require.NoError(t, err)

	require.Equal(t, "Content violates usage guidelines", resp.ToMessage())
}

func TestGeneralErrorResponseDetailOpenAIError(t *testing.T) {
	var resp GeneralErrorResponse
	err := common.Unmarshal([]byte(`{"detail":{"message":"Failed check: SAFETY_CHECK_TYPE","type":"invalid_request_error","code":"bad_request"}}`), &resp)
	require.NoError(t, err)

	openAIError := resp.TryToOpenAIError()
	require.NotNil(t, openAIError)
	require.Equal(t, "Failed check: SAFETY_CHECK_TYPE", openAIError.Message)
	require.Equal(t, "bad_request", openAIError.Code)
}
