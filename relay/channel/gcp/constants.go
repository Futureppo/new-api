package gcp

var ModelList = []string{
	"gcp-text-to-speech",
	"gcp-speech-to-text",
	"tts-1",
	"tts-1-hd",
	"whisper-1",
}

var ChannelName = "google-cloud"

const (
	defaultSpeechBaseURL       = "https://speech.googleapis.com"
	defaultTextToSpeechBaseURL = "https://texttospeech.googleapis.com"

	defaultSTTLanguageCode = "cmn-Hans-CN"
	defaultTTSLanguageCode = "cmn-CN"
	defaultTTSVoiceName    = "cmn-CN-Wavenet-A"
	defaultTTSFormat       = "mp3"

	contextKeyGCPNativeResponse = "gcp_native_response"
	contextKeyGCPResponseFormat = "gcp_response_format"
	contextKeyGCPAudioEncoding  = "gcp_audio_encoding"
)
