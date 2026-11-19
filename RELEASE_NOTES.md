# IC-RADIO v0.7.1

This maintenance release refines the rack-style interface introduced in the
0.7 series, making status information clearer and reducing controls that could
be mistaken for active buttons.

## Highlights

- Redesigned connection indicator with an engraved aluminium panel and a
  clearer illuminated status LED.
- Connected port and PTT line are now grouped into compact read-only telemetry
  instead of appearing as interactive controls.
- Read-only header fields use engraved fascia styling without hover or button
  affordances.
- `PTT SETTINGS` now has a distinct utility-button treatment and gear icon.
- WDSP availability is shown in a brighter engraved status plate with improved
  contrast for ready and unavailable states.
- Small spacing and hierarchy refinements across the top rack panel.

## Download

Download `IC-RADIO-Windows-x64-v0.7.1.zip`, extract the complete folder, and
run `ic-radio.exe`. Keep the `runtime` directory beside the executable so WDSP
can load. The executable is unsigned, so Windows may request confirmation the
first time it is launched.

## Important

The radio must support USB audio and RTS or DTR PTT control. Select the USB
audio input as the radio modulation source. Headphones are recommended when
using `MON` or previewing signalling cues to avoid acoustic feedback.
