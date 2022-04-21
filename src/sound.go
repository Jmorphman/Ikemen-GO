package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/faiface/beep"
	"github.com/faiface/beep/effects"

	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/vorbis"
	"github.com/faiface/beep/wav"
)

const (
	audioOutLen    = 2048
	audioFrequency = 48000
	audioPrecision = 4
	audioResampleQuality = 3
)

// ------------------------------------------------------------------
// Normalizer

type Normalizer struct {
	streamer beep.Streamer
	mul  float64
	l, r *NormalizerLR
}

func NewNormalizer(st beep.Streamer) *Normalizer {
	return &Normalizer{streamer: st, mul: 4,
		l: &NormalizerLR{1, 0, 1, 1 / 32.0, 0, 0},
		r: &NormalizerLR{1, 0, 1, 1 / 32.0, 0, 0}}
}

func (n *Normalizer) Stream(samples [][2]float64) (s int, ok bool) {
	s, ok = n.streamer.Stream(samples)
	for i:= range samples[:s] {
		lmul := n.l.process(n.mul, &samples[i][0])
		rmul := n.r.process(n.mul, &samples[i][1])
		if sys.audioDucking {
			n.mul = math.Min(16.0, math.Min(lmul, rmul))
		} else {
			n.mul = 0.5 * (float64(sys.wavVolume) * float64(sys.masterVolume) * 0.0001)
		}
	}
	return s, ok
}

func (n *Normalizer) Err() error {
        return n.streamer.Err()
}

type NormalizerLR struct {
	heri, herihenka, fue, heikin, katayori, katayori2 float64
}

func (n *NormalizerLR) process(bai float64, sam *float64) float64 {
	n.katayori += (*sam - n.katayori) / (audioFrequency/110.0 + 1)
	n.katayori2 += (*sam - n.katayori2) / (audioFrequency/112640.0 + 1)
	s := (n.katayori2 - n.katayori) * bai
	if math.Abs(s) > 1 {
		bai *= math.Pow(math.Abs(s), -n.heri)
		n.herihenka += 32 * (1 - n.heri) / float64(audioFrequency+32)
		s = math.Copysign(1.0, s)
	} else {
		tmp := (1 - math.Pow(1-math.Abs(s), 64)) * math.Pow(0.5-math.Abs(s), 3)
		bai += bai * (n.heri*(1/32.0-n.heikin)/n.fue + tmp*n.fue*(1-n.heri)/32) /
			(audioFrequency*2/8.0 + 1)
		n.herihenka -= (0.5 - n.heikin) * n.heri / (audioFrequency * 2)
	}
	n.fue += (1.0 - n.fue*(math.Abs(s)+1/32.0)) / (audioFrequency * 2)
	n.heikin += (math.Abs(s) - n.heikin) / (audioFrequency * 2)
	n.heri += n.herihenka
	if n.heri < 0 {
		n.heri = 0
	} else if n.heri > 0 {
		n.heri = 1
	}
	*sam = s
	return bai
}

// ------------------------------------------------------------------
// Bgm

type Bgm struct {
	filename     string
	bgmVolume    int
	bgmLoopStart int
	bgmLoopEnd   int
	loop         int
	streamer  beep.StreamSeekCloser
	ctrl      *beep.Ctrl
	volctrl   *effects.Volume
	format    string
}

func newBgm() *Bgm {
	return &Bgm{}
}

func (bgm *Bgm) Open(filename string, loop, bgmVolume, bgmLoopStart, bgmLoopEnd int) {
	bgm.filename = filename
	bgm.loop = loop
	bgm.bgmVolume = bgmVolume
	bgm.bgmLoopStart = bgmLoopStart
	bgm.bgmLoopEnd = bgmLoopEnd
	// Starve the current music streamer
	if bgm.ctrl != nil {
		speaker.Lock()
		bgm.ctrl.Streamer = nil
		speaker.Unlock()
	}
	// Special value "" is used to stop music
	if filename == "" {
		return
	}

	f, err := os.Open(bgm.filename)
	if err != nil {
		sys.errLog.Printf("Failed to open bgm: %v", err)
		return
	}
	var format beep.Format
	if HasExtension(bgm.filename, ".ogg") {
		bgm.streamer, format, err = vorbis.Decode(f)
		bgm.format = "ogg"
	} else if HasExtension(bgm.filename, ".mp3") {
		bgm.streamer, format, err = mp3.Decode(f)
		bgm.format = "mp3"
	} else if HasExtension(bgm.filename, ".wav") {
		bgm.streamer, format, err = wav.Decode(f)
		bgm.format = "wav"
	// TODO: Reactivate FLAC support. Check that seeking/looping works correctly.
	//} else if HasExtension(bgm.filename, ".flac") {
	//	bgm.streamer, format, err = flac.Decode(f)
	//	bgm.format = "flac"
	} else {
		err = Error(fmt.Sprintf("unsupported file extension: %v", bgm.filename))
	}
	if err != nil {
		f.Close()
		sys.errLog.Printf("Failed to load bgm: %v", err)
		return
	}

	loopCount := int(1)
	if loop > 0 {
		loopCount = -1
	}
	streamer := beep.Loop(loopCount, bgm.streamer)
	bgm.volctrl = &effects.Volume{Streamer: streamer, Base: 2, Volume: 0, Silent: true}
	resampler := beep.Resample(audioResampleQuality, format.SampleRate, audioFrequency, bgm.volctrl)
	bgm.ctrl = &beep.Ctrl{Streamer: resampler}
	bgm.UpdateVolume()
	speaker.Play(bgm.ctrl)
}

func (bgm *Bgm) Pause() {
	// FIXME: there is no method to unpause!
	if bgm.ctrl == nil {
		return
	}
	speaker.Lock()
	bgm.ctrl.Paused = true
	speaker.Unlock()
}

func (bgm *Bgm) UpdateVolume() {
	if bgm.volctrl == nil {
		return
	}
	// TODO: Throw a debug warning if this triggers
	if bgm.bgmVolume > sys.maxBgmVolume {
		bgm.bgmVolume = sys.maxBgmVolume
	}
	volume := -5 + float64(sys.bgmVolume)*0.06*(float64(sys.masterVolume)/100)*(float64(bgm.bgmVolume)/100)
	silent := volume <= -5
	speaker.Lock()
	bgm.volctrl.Volume = volume
	bgm.volctrl.Silent = silent
	speaker.Unlock()
}

// ------------------------------------------------------------------
// Sound

type Sound struct {
	Buffer *beep.Buffer
}

func newSound(sampleRate beep.SampleRate) *Sound {
	fmt := beep.Format{SampleRate: sampleRate, NumChannels: 2, Precision: audioPrecision}
	return &Sound{beep.NewBuffer(fmt)}
}

func readSound(f *os.File, ofs int64) (*Sound, error) {
	s, fmt, err := wav.Decode(f)
	if err != nil {
		return nil, err
	}
	sound := newSound(fmt.SampleRate)
	sound.Buffer.Append(s)
	return sound, nil
}

func (s *Sound) Play() bool {
	c := sys.soundChannels.reserveChannel()
	if c == nil {
		return false
	}
	c.Play(s, false, 1.0)
	return s != nil
}

func (s *Sound) GetDuration() float32 {
	return float32(s.Buffer.Format().SampleRate.D(s.Buffer.Len()))
}

// ------------------------------------------------------------------
// Snd

type Snd struct {
	table     map[[2]int32]*Sound
	ver, ver2 uint16
}

func newSnd() *Snd {
	return &Snd{table: make(map[[2]int32]*Sound)}
}

func LoadSnd(filename string) (*Snd, error) {
	return LoadSndFiltered(filename, func(gn [2]int32) bool { return gn[0] >= 0 && gn[1] >= 0 }, 0)
}

// Parse a .snd file and return an Snd structure with its contents
// The "keepItem" function allows to filter out unwanted waves.
// If max > 0, the function returns immediately when a matching entry is found. It also gives up after "max" non-matching entries.
func LoadSndFiltered(filename string, keepItem func([2]int32) bool, max uint32) (*Snd, error) {
	s := newSnd()
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { chk(f.Close()) }()
	buf := make([]byte, 12)
	var n int
	if n, err = f.Read(buf); err != nil {
		return nil, err
	}
	if string(buf[:n]) != "ElecbyteSnd\x00" {
		return nil, Error("Unrecognized SND file, invalid header")
	}
	read := func(x interface{}) error {
		return binary.Read(f, binary.LittleEndian, x)
	}
	if err := read(&s.ver); err != nil {
		return nil, err
	}
	if err := read(&s.ver2); err != nil {
		return nil, err
	}
	var numberOfSounds uint32
	if err := read(&numberOfSounds); err != nil {
		return nil, err
	}
	var subHeaderOffset uint32
	if err := read(&subHeaderOffset); err != nil {
		return nil, err
	}
	loops := numberOfSounds
	if max > 0 && max < numberOfSounds {
		loops = max
	}
	for i := uint32(0); i < loops; i++ {
		f.Seek(int64(subHeaderOffset), 0)
		var nextSubHeaderOffset uint32
		if err := read(&nextSubHeaderOffset); err != nil {
			return nil, err
		}
		var subFileLenght uint32
		if err := read(&subFileLenght); err != nil {
			return nil, err
		}
		var num [2]int32
		if err := read(&num); err != nil {
			return nil, err
		}
		if keepItem(num) {
			_, ok := s.table[num]
			if !ok {
				tmp, err := readSound(f, int64(subHeaderOffset))
				if err != nil {
					sys.errLog.Printf("%v sound can't be read: %v,%v\n", filename, num[0], num[1])
					if max > 0 {
						return nil, err
					}
				} else {
					s.table[num] = tmp
					if max > 0 {
						break
					}
				}
			}
		}
		subHeaderOffset = nextSubHeaderOffset
	}
	return s, nil
}
func (s *Snd) Get(gn [2]int32) *Sound {
	return s.table[gn]
}
func (s *Snd) play(gn [2]int32, volumescale int32, pan float32) bool {
	c := sys.soundChannels.reserveChannel()
	if c == nil {
		return false
	}
	sound := s.Get(gn)
	c.Play(sound, false, 1.0)
	c.SetVolume(float32(volumescale * 64 / 25))
	c.SetPan(pan, 0, nil)
	return sound != nil
}
func (s *Snd) stop(gn [2]int32) {
	sys.soundChannels.stop(s.Get(gn))
}

func loadFromSnd(filename string, g, s int32, max uint32) (*Sound, error) {
	// Load the snd file
	snd, err := LoadSndFiltered(filename, func(gn [2]int32) bool { return gn[0] == g && gn[1] == s }, max)
	if err != nil {
		return nil, err
	}
	tmp, ok := snd.table[[2]int32{g, s}]
	if !ok {
		return newSound(11025), nil
	}
	return tmp, nil
}

// ------------------------------------------------------------------
// SoundEffect (handles volume and panning)

type SoundEffect struct {
	streamer beep.Streamer
	volume float32
	ls, p float32
	x *float32
}

func (s *SoundEffect) Stream(samples [][2]float64) (n int, ok bool) {
	// TODO: Test mugen panning in relation to PanningWidth and zoom settings
	lv, rv := s.volume, s.volume
	if sys.stereoEffects && (s.x != nil || s.p != 0) {
		var r float32
		if s.x != nil { // pan
			r = ((sys.xmax - s.ls**s.x) - s.p) / (sys.xmax - sys.xmin)
		} else { // abspan
			r = ((sys.xmax-sys.xmin)/2 - s.p) / (sys.xmax - sys.xmin)
		}
		sc := sys.panningRange / 100
		of := (100 - sys.panningRange) / 200
		lv = s.volume * 2 * (r*sc + of)
		rv = s.volume * 2 * ((1-r)*sc + of)
		if lv > 512 {
			lv = 512
		} else if lv < 0 {
			lv = 0
		}
		if rv > 512 {
			rv = 512
		} else if rv < 0 {
			rv = 0
		}
	}

	n, ok = s.streamer.Stream(samples)
	for i:= range samples[:n] {
		samples[i][0] *= float64(lv / 256)
		samples[i][1] *= float64(rv / 256)
	}
	return n, ok
}

func (s *SoundEffect) Err() error {
	return s.streamer.Err()
}

// ------------------------------------------------------------------
// SoundChannel

type SoundChannel struct {
	streamer  beep.StreamSeeker
	sfx     *SoundEffect
	ctrl    *beep.Ctrl
	sound   *Sound
}

func (s *SoundChannel) Play(sound *Sound, loop bool, freqmul float32) {
	if sound == nil {
		return
	}
	s.sound = sound
	s.streamer = s.sound.Buffer.Streamer(0, s.sound.Buffer.Len())
	loopCount := int(1)
	if loop {
		loopCount = -1
	}
	looper := beep.Loop(loopCount, s.streamer)
	s.sfx = &SoundEffect{streamer: looper, volume: 256}
	srcRate := s.sound.Buffer.Format().SampleRate
	dstRate := beep.SampleRate(audioFrequency / freqmul)
	resampler := beep.Resample(audioResampleQuality, srcRate, dstRate, s.sfx)
	s.ctrl = &beep.Ctrl{Streamer: resampler}
	speaker.Play(s.ctrl)
}
func (s *SoundChannel) IsPlaying() bool {
	return s.sound != nil
}
func (s *SoundChannel) Stop() {
	if s.ctrl != nil {
		speaker.Lock()
		s.ctrl.Streamer = nil
		speaker.Unlock()
	}
	s.sound = nil
}
func (s *SoundChannel) SetVolume(vol float32) {
	if s.ctrl != nil {
		s.sfx.volume = float32(math.Max(0, math.Min(float64(vol), 512)))
	}
}
func (s *SoundChannel) SetPan(p, ls float32, x *float32) {
	if s.ctrl != nil {
		s.sfx.ls = ls
		s.sfx.x = x
		s.sfx.p = p * ls
	}
}

// ------------------------------------------------------------------
// SoundChannels (collection of prioritised sound channels)

type SoundChannels struct {
	channels []SoundChannel
}

func newSoundChannels(size int32) *SoundChannels {
	s := &SoundChannels{}
	s.SetSize(size)
	return s
}
func (s *SoundChannels) SetSize(size int32)  {
	if size > s.count() {
		c := make([]SoundChannel, size - s.count())
		s.channels = append(s.channels, c...)
	} else if size < s.count() {
		for i := s.count()-1; i >= size; i-- {
			s.channels[i].Stop()
		}
		s.channels = s.channels[:size]
	}
}
func (s *SoundChannels) count() int32 {
	return int32(len(s.channels))
}
func (s *SoundChannels) New(ch int32, lowpriority bool) *SoundChannel {
        ch = Min(255, ch)
        if ch >= 0 {
                if lowpriority {
                        if s.count() > ch && s.channels[ch].IsPlaying() {
                                return nil
                        }
                }
                if s.count() < ch+1 {
			s.SetSize(ch+1)
                }
		s.channels[ch].Stop()
                return &s.channels[ch]
        }
        if s.count() < 256 {
		s.SetSize(256)
        }
        for i := 255; i >= 0; i-- {
                if !s.channels[i].IsPlaying() {
                        return &s.channels[i]
                }
        }
        return nil
}
func (s *SoundChannels) reserveChannel() *SoundChannel {
	for i := range s.channels {
		if !s.channels[i].IsPlaying() {
			return &s.channels[i]
		}
	}
	return nil
}
func (s *SoundChannels) Get(ch int32) *SoundChannel {
	if ch >= 0 && ch < s.count() {
		return &s.channels[ch]
	}
	return nil
}
func (s *SoundChannels) IsPlaying(sound *Sound) bool {
	for _, v := range s.channels {
		if v.sound != nil && v.sound == sound {
			return true
		}
	}
	return false
}
func (s *SoundChannels) stop(sound *Sound) {
	for k, v := range s.channels {
		if v.sound != nil && v.sound == sound {
			s.channels[k].Stop()
		}
	}
}
func (s *SoundChannels) StopAll() {
	for k, v := range s.channels {
		if v.sound != nil {
			s.channels[k].Stop()
		}
	}
}
func (s *SoundChannels) Tick() {
	for i := range s.channels {
		if s.channels[i].IsPlaying() {
			if s.channels[i].streamer.Position() >= s.channels[i].sound.Buffer.Len() {
				s.channels[i].sound = nil
			}
		}
	}
}
