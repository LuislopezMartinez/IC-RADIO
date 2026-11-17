# Third-party notices

## WDSP 2.11

IC-RADIO can load the WDSP signal-processing library by Warren C. Pratt,
NR0V. The portable runtime in `runtime/wdsp.dll` was built from the official
TAPR/OpenHPSDR-wdsp source at commit
`e69de50d1d702216098019013899ad87534c64e0` (WDSP 2.11).

Source: https://github.com/TAPR/OpenHPSDR-wdsp

Portability patch: the Windows runtime replaces FFTW's `PATIENT` planner flag
with `ESTIMATE`. This removes the machine-specific, multi-minute wisdom build
at first start; it does not alter WDSP's filtering or neural-network code.

IC-RADIO also compiles `native/wdsp/ic_audio_bridge.c` into the DLL. This thin
GPL adapter sends already-demodulated USB PCM directly through WDSP's NNR,
EMNR, ANF and SNBA stages instead of using its SDR I/Q demodulation channel.

License: GNU General Public License, version 2 or later. A copy is included in
`THIRD_PARTY_LICENSES/WDSP/GPL-2.0.txt`.

## FFTW 3

`runtime/libfftw3-3.dll` is the double-precision FFTW runtime used by WDSP. It
was built and packaged by the MSYS2 mingw-w64-ucrt-x86_64-fftw package.

Source: https://www.fftw.org/

License: GNU General Public License, version 2 or later. A copy is included in
`THIRD_PARTY_LICENSES/FFTW/GPL-2.0.txt`.
