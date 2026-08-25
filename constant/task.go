package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
	TaskActionPPT               = "ppt"
	TaskActionPSD               = "psd"
	TaskActionImageGeneration   = "image_generation"
	TaskActionImageEdit         = "image_edit"
	TaskActionAudioGeneration   = "audio_generation"
	TaskActionMusicGeneration   = "music_generation"
	TaskActionVoiceClone        = "voice_clone"
	TaskActionBatchInference    = "batch_inference"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
