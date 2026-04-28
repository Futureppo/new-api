package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGetEndpointTypesByChannelTypeCohere(t *testing.T) {
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeCohereChat},
		GetEndpointTypesByChannelType(constant.ChannelTypeCohere, "command-a-03-2025"),
	)
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeCohereRerank},
		GetEndpointTypesByChannelType(constant.ChannelTypeCohere, "rerank-v3.5"),
	)
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeCohereEmbeddings},
		GetEndpointTypesByChannelType(constant.ChannelTypeCohere, "embed-v4.0"),
	)
}
