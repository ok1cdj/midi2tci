package ctrl

import (
	"log"
	"time"

	"github.com/ftl/tci/client"
)

type MidiKey struct {
	Channel byte
	Key     byte
}

type LED interface {
	Set(key MidiKey, on bool)
}

func NewMuteButton(key MidiKey, led LED, muter Muter) *MuteButton {
	return &MuteButton{
		key:   key,
		led:   led,
		muter: muter,
	}
}

type MuteButton struct {
	key   MidiKey
	led   LED
	muter Muter

	muted bool
}

type Muter interface {
	SetMute(bool) error
}

func (b *MuteButton) Pressed() {
	err := b.muter.SetMute(!b.muted)
	if err != nil {
		log.Print(err)
	}
}

func (b *MuteButton) SetMute(muted bool) {
	b.muted = muted
	b.led.Set(b.key, !muted)
}

func NewRXChannelEnableButton(key MidiKey, trx int, vfo client.VFO, led LED, rxChannelEnabler RXChannelEnabler) *RXChannelEnableButton {
	return &RXChannelEnableButton{
		key:              key,
		trx:              trx,
		vfo:              vfo,
		led:              led,
		rxChannelEnabler: rxChannelEnabler,
	}
}

type RXChannelEnableButton struct {
	key              MidiKey
	trx              int
	vfo              client.VFO
	led              LED
	rxChannelEnabler RXChannelEnabler

	enabled bool
}

type RXChannelEnabler interface {
	SetRXChannelEnable(int, client.VFO, bool) error
}

func (b *RXChannelEnableButton) Pressed() {
	err := b.rxChannelEnabler.SetRXChannelEnable(b.trx, b.vfo, !b.enabled)
	if err != nil {
		log.Print(err)
	}
}

func (b *RXChannelEnableButton) SetRXChannelEnable(trx int, vfo client.VFO, enabled bool) {
	if trx != b.trx || vfo != b.vfo {
		return
	}
	b.enabled = enabled
	b.led.Set(b.key, enabled)
}

func NewSplitEnableButton(key MidiKey, trx int, led LED, splitEnabler SplitEnabler) *SplitEnableButton {
	return &SplitEnableButton{
		key:          key,
		trx:          trx,
		led:          led,
		splitEnabler: splitEnabler,
	}
}

type SplitEnableButton struct {
	key          MidiKey
	trx          int
	led          LED
	splitEnabler SplitEnabler

	enabled bool
}

type SplitEnabler interface {
	SetSplitEnable(int, bool) error
}

func (b *SplitEnableButton) Pressed() {
	err := b.splitEnabler.SetSplitEnable(b.trx, !b.enabled)
	if err != nil {
		log.Print(err)
	}
}

func (b *SplitEnableButton) SetSplitEnable(trx int, enabled bool) {
	if trx != b.trx {
		return
	}
	b.enabled = enabled
	b.led.Set(b.key, enabled)
}

func NewVFOWheel(key MidiKey, trx int, vfo client.VFO, controller VFOFrequencyController) *VFOWheel {
	result := &VFOWheel{
		key:        key,
		trx:        trx,
		vfo:        vfo,
		controller: controller,
		frequency:  make(chan int, 1000),
		turns:      make(chan int, 1000),
		closed:     make(chan struct{}),
	}

	go func() {
		defer close(result.closed)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		accumulatedTurns := 0
		turning := false
		frequency := 0
		for {
			select {
			case turns, valid := <-result.turns:
				if !valid {
					return
				}
				accumulatedTurns += turns
				turning = frequency > 0
			case f := <-result.frequency:
				if !turning {
					frequency = f
				}
			case <-ticker.C:
				if accumulatedTurns == 0 {
					turning = false
				} else if accumulatedTurns != 0 && frequency != 0 {
					frequency = frequency + int(float64(accumulatedTurns)*1.8)
					err := result.controller.SetVFOFrequency(result.trx, result.vfo, frequency)
					if err != nil {
						log.Printf("Cannot change frequency to %d: %v", result.frequency, err)
					}
					accumulatedTurns = 0
				}
			}
		}
	}()

	return result
}

type VFOWheel struct {
	key        MidiKey
	trx        int
	vfo        client.VFO
	controller VFOFrequencyController

	frequency chan int
	turns     chan int
	closed    chan struct{}
}

type VFOFrequencyController interface {
	SetVFOFrequency(trx int, vfo client.VFO, frequency int) error
}

func (w *VFOWheel) Close() {
	select {
	case <-w.closed:
		return
	default:
		close(w.turns)
		<-w.closed
	}
}

func (w *VFOWheel) Turned(turns int) {
	w.turns <- turns
}

func (w *VFOWheel) SetVFOFrequency(trx int, vfo client.VFO, frequency int) {
	if trx != w.trx || vfo != w.vfo {
		return
	}
	w.frequency <- frequency
}

func NewSlider(set func(int), translate func(int) int) *Slider {
	result := &Slider{
		set:           set,
		translate:     translate,
		selectedValue: make(chan int, 1000),
		activeValue:   make(chan int, 1000),
		closed:        make(chan struct{}),
	}

	result.start()

	return result
}

type Slider struct {
	set           func(int)
	translate     func(int) int
	activeValue   chan int
	selectedValue chan int
	closed        chan struct{}
}

func (s *Slider) start() {
	tx := make(chan int)
	go func() {
		for {
			select {
			case <-s.closed:
				return
			case value := <-tx:
				s.set(value)
			}
		}
	}()

	go func() {
		defer close(s.closed)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		activeValue := 0
		selectedValue := 0
		pending := false

		for {
			select {
			case value, valid := <-s.activeValue:
				if !valid {
					return
				}
				activeValue = value
				if !pending {
					selectedValue = activeValue
				}
			case value, valid := <-s.selectedValue:
				if !valid {
					return
				}
				selectedValue = value

				if activeValue == selectedValue {
					continue
				}

				select {
				case tx <- selectedValue:
					pending = false
				default:
					pending = true
				}
			case <-ticker.C:
				if activeValue == selectedValue {
					pending = false
					continue
				}

				select {
				case tx <- selectedValue:
					pending = false
				default:
					pending = true
				}
			}
		}
	}()
}

func (s *Slider) Close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.activeValue)
		close(s.selectedValue)
		<-s.closed
	}
}

func (s *Slider) Changed(value int) {
	s.selectedValue <- s.translate(value)
}

func NewVolumeSlider(controller VolumeController) *VolumeSlider {
	const tick = float64(60.0 / 127.0)
	return &VolumeSlider{
		Slider: NewSlider(
			func(v int) {
				err := controller.SetVolume(v)
				if err != nil {
					log.Printf("Cannot change volume: %v", err)
				}
			},
			func(v int) int { return -60 + int(float64(v)*tick) },
		),
	}
}

type VolumeSlider struct {
	*Slider
}

type VolumeController interface {
	SetVolume(dB int) error
}

func (s *VolumeSlider) SetVolume(volume int) {
	s.activeValue <- volume
}
