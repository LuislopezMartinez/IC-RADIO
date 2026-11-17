# IC-RADIO v0.7.0

This release substantially expands RX processing and adds programmable audio
signalling around PTT operation.

## Highlights

- Portable WDSP 2.11 receive engine with NR2 and NNR noise reduction.
- Standard and Premium NNR models, automatic notch filtering, impulse-noise
  blanking, adjustable reduction, and signal protection.
- Safe automatic fallback to the classic DSP if the native WDSP runtime is not
  available or stops processing.
- True RX noise-reduction bypass without accidentally enabling two reducers in
  series.
- New Sonic Sequencer for configurable PTT opening and closing cues.
- Build cues from tones, pauses, sweeps, or an automatically generated Morse
  callsign; reorder blocks and preview the complete sequence in RX.
- Piano-style note selection, sine/square/triangle waveforms, octave controls,
  selectable durations, and adjustable RX cue volume.
- Click-free cue envelopes and isolation from stale voice-DSP or echo state.
- RX and TX selectors now change the editor without interrupting live audio or
  altering the physical PTT state.
- Distinct RX and TX editor accent colours and expanded regression coverage.
- Reproducible WDSP build script pinned to the official upstream source, with
  bundled FFTW runtime, third-party notices, and GPL license copies.

## Download

Download `IC-RADIO-Windows-x64-v0.7.0.zip`, extract the complete folder, and
run `ic-radio.exe`. Keep the `runtime` directory beside the executable so WDSP
can load. The executable is unsigned, so Windows may request confirmation the
first time it is launched.

## Important

The radio must support USB audio and RTS or DTR PTT control. Select the USB
audio input as the radio modulation source. Headphones are recommended when
using `MON` or previewing signalling cues to avoid acoustic feedback.
