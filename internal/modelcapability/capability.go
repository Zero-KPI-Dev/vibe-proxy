package modelcapability

type SupportState string

const (
	SupportUnknown     SupportState = "unknown"
	SupportSupported   SupportState = "supported"
	SupportUnsupported SupportState = "unsupported"
)

func (s SupportState) Valid() bool {
	switch s {
	case "", SupportUnknown, SupportSupported, SupportUnsupported:
		return true
	default:
		return false
	}
}

func (s SupportState) Effective() SupportState {
	if s == "" {
		return SupportUnknown
	}
	return s
}

type ModelCapabilities struct {
	ImageInput SupportState `yaml:"image_input,omitempty" json:"image_input,omitempty"`
}

type Source string

const (
	SourceModelOverride   Source = "model_override"
	SourceProviderDefault Source = "provider_default"
	SourceModelsDev       Source = "models_dev"
	SourceUnknown         Source = "unknown"
)
