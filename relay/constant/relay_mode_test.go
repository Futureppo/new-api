package constant

import "testing"

func TestPath2RelayModeVideoEndpoints(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "/v1/video/generations", want: RelayModeVideoSubmit},
		{path: "/v1/videos/generations", want: RelayModeVideoSubmit},
		{path: "/v1/videos", want: RelayModeVideoSubmit},
		{path: "/v1/videos/video_123/remix", want: RelayModeVideoSubmit},
		{path: "/v1/video/generations/task_123", want: RelayModeVideoFetchByID},
		{path: "/v1/videos/task_123", want: RelayModeVideoFetchByID},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := Path2RelayMode(tt.path); got != tt.want {
				t.Fatalf("Path2RelayMode(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}
