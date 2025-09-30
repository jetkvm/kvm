package native

type VideoState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` //no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"fps"`
}

func (n *Native) VideoSetQualityFactor(factor float64) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoSetStreamQualityFactor(factor)
}

func (n *Native) VideoGetQualityFactor() (float64, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetStreamQualityFactor()
}

func (n *Native) VideoSetEDID(edid string) error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoSetEDID(edid)
}

func (n *Native) VideoGetEDID() (string, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoGetEDID()
}

func (n *Native) VideoLogStatus() (string, error) {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	return videoLogStatus(), nil
}

func (n *Native) VideoStop() error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	videoStop()
	return nil
}

func (n *Native) VideoStart() error {
	n.videoLock.Lock()
	defer n.videoLock.Unlock()

	videoStart()
	return nil
}
