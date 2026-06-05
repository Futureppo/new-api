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

func TestGetEndpointTypesByChannelTypeVolcEngine(t *testing.T) {
	tests := []struct {
		model string
		want  []constant.EndpointType
	}{
		{
			model: "doubao-seed-2-0-pro-260215",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAI},
		},
		{
			model: "deepseek-v4-flash-260425",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAI},
		},
		{
			model: "doubao-embedding-text-240715",
			want:  []constant.EndpointType{constant.EndpointTypeEmbeddings},
		},
		{
			model: "doubao-embedding-vision-251215",
			want:  []constant.EndpointType{constant.EndpointTypeEmbeddings},
		},
		{
			model: "doubao-seedream-5-0-260128",
			want:  []constant.EndpointType{constant.EndpointTypeImageGeneration, constant.EndpointTypeOpenAI},
		},
		{
			model: "doubao-seededit-3-0-i2i-250628",
			want:  []constant.EndpointType{constant.EndpointTypeImageGeneration, constant.EndpointTypeOpenAI},
		},
		{
			model: "doubao-seedance-2-0-fast-260128",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		},
		{
			model: "wan2-1-14b-i2v-250225",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		},
		{
			model: "doubao-seed3d-2-0-260328",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		},
		{
			model: "hyper3d-gen2-260112",
			want:  []constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			require.Equal(t, tt.want, GetEndpointTypesByChannelType(constant.ChannelTypeVolcEngine, tt.model))
		})
	}
}
