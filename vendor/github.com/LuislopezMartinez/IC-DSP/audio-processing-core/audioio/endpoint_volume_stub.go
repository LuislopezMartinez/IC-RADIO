//go:build !windows || !cgo

package audioio

type PlaybackVolumeStatus struct {
	Percent float64
	Muted   bool
}

func InspectPlaybackEndpoint(string) (PlaybackVolumeStatus, error) {
	return PlaybackVolumeStatus{Percent: 100}, nil
}

func DefaultPlaybackEndpointName() (string, error) { return "", nil }
