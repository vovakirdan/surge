package buildpipeline

// ChannelSink forwards events into a channel.
type ChannelSink struct {
	Ch chan<- Event
}

// OnEvent forwards the event to the channel.
func (s ChannelSink) OnEvent(evt Event) {
	if s.Ch == nil {
		return
	}
	s.Ch <- evt
}
