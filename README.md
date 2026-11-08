# IC-RADIO

RX/TX audio console for USB-equipped transceivers, featuring real-time DSP and
PTT control through RTS or DTR. IC-RADIO lets you use an external USB
microphone, process your voice, and route it to the radio USB audio input.

![IC-RADIO interface](assets/screenshots/ic-radio-interface.png)

## Features

- Independent RX and TX audio routes with drop-down device selectors.
- Logarithmic spectrum, input/output meters, and draggable filter markers.
- Eleven-band graphic equalizer up to 10 kHz, with reset and bypass.
- Adjustable high-pass and low-pass voice filters.
- RX notch filter with draggable frequency, width, and depth controls.
- SSB voice emphasis and 50/75 µs FM de-emphasis.
- TX compressor, configurable noise reduction, output gain, and presence.
- TX echo with delay, feedback, mix, tone profiles, and quick presets.
- Formant-preserving voice shifter from -4 to +4 semitones.
- Natural, Balanced, and Intense voice-shifter modes, automatic level,
  consonant protection, voice activity bypass, clarity, and A/B monitoring.
- `MON` output for listening to processed TX audio through the RX headphones.
- Monitor clock-drift compensation and risky-route feedback detection.
- Progressive TX saturation protection with manual safety reset.
- Automatic 100% volume and unmute preparation for the radio USB endpoint.
- PTT over RTS or DTR with Hold and Lock modes, configurable shortcut, and
  emergency return to RX with Escape.
- Persistent independent RX/TX settings and three named global presets.
- Self-contained source tree: the DSP core and all Go dependencies are
  included locally under `vendor`.

## Compatibility

IC-RADIO works with transceivers that expose:

1. A USB recording endpoint carrying audio from the radio.
2. A USB playback endpoint accepting modulation audio.
3. A serial port capable of PTT control through RTS or DTR.

Device names and radio menus vary by manufacturer. Select USB audio as the
radio modulation source and assign PTT/SEND to the same RTS or DTR line chosen
in IC-RADIO. The Icom IC-7300 is supported but is not required.

## Windows installation

Download `IC-RADIO-Windows-x64-v0.6.0.zip` from **Releases**, extract it, and
run `ic-radio.exe`. Windows may display a warning because the executable is not
digitally signed.

Settings are stored in `%AppData%\IC-MIC\settings.json`.

> Headphones are recommended when using `MON`; loudspeakers can create acoustic
> feedback through the microphone.

## Building locally

Requirements: Go 1.22 or later, CGO, and a Windows-compatible C compiler. No
external IC-DSP checkout or Go module download is required.

```powershell
.\build-radio-windows.ps1
```

## PTT safety

IC-RADIO clears RTS and DTR when opening and closing the serial port. Escape
forces an immediate return to RX, and Hold mode returns to RX automatically if
the application loses focus.

## Credits

Created by EA3IWW. The embedded DSP core originates from IC-DSP. The interface
includes Inter and Black Ops One under the SIL Open Font License 1.1.
