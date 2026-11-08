# IC-RADIO v0.6.0

This release focuses on voice processing, reliable TX routing, monitoring
safety, and an international English interface.

## Highlights

- Advanced formant-preserving TX voice shifter with Natural, Balanced, and
  Intense modes.
- Adjustable pitch and clarity, automatic level compensation, voice activity
  bypass, consonant protection, and momentary A/B comparison.
- Stabilized LPC synthesis with automatic recovery, preventing the voice
  shifter from muting the radio and monitor routes.
- Progressive saturation protection that reduces excessive TX level without
  ever replacing the audio stream with silence.
- Manual `RESET SAFE` control and an effective 18 dB combined gain/presence
  ceiling.
- Automatic unmute and 100% volume preparation for dedicated `USB Audio CODEC`
  radio outputs.
- `MON` clock-drift compensation for cleaner long monitoring sessions.
- Detection and blocking of risky loopback routes that could cause feedback.
- Compact redesigned TX filter and voice-shifter controls.
- Automatic button text fitting and fully English user interface.
- `By EA3IWW` branding in the application header and window title.
- Updated screenshot and expanded regression tests for complex voice signals,
  routing, safety protection, persistence, and DSP behavior.

## Download

Download `IC-RADIO-Windows-x64-v0.6.0.zip`, extract it, and run
`ic-radio.exe`. The executable is unsigned, so Windows may request confirmation
the first time it is launched.

## Important

The radio must support USB audio and RTS or DTR PTT control. Select the USB
audio input as the radio modulation source. Headphones are recommended when
using `MON` to avoid acoustic feedback.
