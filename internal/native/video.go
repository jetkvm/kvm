package native

type VideoState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

func (n *Native) VideoSetQualityFactor(factor float64) error {
	return videoSetStreamQualityFactor(factor)
}

func (n *Native) VideoGetQualityFactor() (float64, error) {
	return videoGetStreamQualityFactor()
}

func (n *Native) VideoSetEDID(edid string) error {
	return videoSetEDID(edid)
}

func (n *Native) VideoGetEDID() (string, error) {
	return videoGetEDID()
}
