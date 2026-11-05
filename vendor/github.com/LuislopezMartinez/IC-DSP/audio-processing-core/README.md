# Audio Processing Core

Biblioteca Go independiente, sin dependencias externas, extraída del procesamiento de PBT / AUDIO de GO-Zero. Está pensada para incorporarse desde otro programa y no contiene interfaz gráfica, reproducción, captura ni configuración persistente.

## Incluye

- Reducción de ruido espectral adaptativa con intensidad 0–100, velocidades `slow`, `normal` y `fast`, transición suave y telemetría.
- Ecualizador paramétrico de cinco bandas: 120, 300, 800, 2000 y 5000 Hz.
- Filtro de paso de audio configurable (corte bajo y alto).
- Notch manual con frecuencia, ancho y profundidad configurables, transición sin clics y detector automático de tonos.
- Espectro de audio en dBFS, antes del notch y a la salida del procesador.
- Preénfasis y desénfasis FM seleccionables de 50 o 75 µs.
- Crossfade de potencia constante entre la entrada directa y la cadena procesada, ajustable de 0 a 100 %.
- Twin PBT para USB/LSB a partir de IQ intercalado.
- Limitador de seguridad y protección frente a NaN/Inf.
- Biblioteca de interfaz `simpleui`, con controles, temas, fuentes embebidas y canvas escalable sobre raylib-go.

## Uso rápido

```go
import audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"

processor := audio.NewProcessor(48_000)
cfg := audio.DefaultConfig()
cfg.NoiseEnabled = true
cfg.Notch.Enabled = true
cfg.Emphasis = audio.DeEmphasis
cfg.EmphasisMicroseconds = 75
processor.Configure(cfg)

// PCM mono normalizado en [-1, 1]. El bloque se modifica in situ.
processor.Process(samples)
```

`ProcessedMix` controla el crossfade: `0` entrega la entrada directa, `100` la señal completamente procesada y los valores intermedios combinan ambas rutas sin provocar una caída brusca de nivel en el centro. `OutputGainDB` se aplica después de la mezcla.

Para consumir esta carpeta desde otro módulo local:

```go
require github.com/LuislopezMartinez/IC-DSP/audio-processing-core v0.1.0
```

Para distribución real conviene mover la carpeta a su propio repositorio y sustituir el nombre del módulo en `go.mod` por la URL del repositorio.

### Interfaz SimpleUI

```go
import ui "github.com/LuislopezMartinez/IC-DSP/audio-processing-core/simpleui"

ui.SetMode(1280, 720, ui.Fit)
ui.SetTitle("Procesador de audio")
start := ui.NewButton("start", 40, 40, 180, 44, "INICIAR", 16)
ui.Add(start)
```

La guía completa está en [`simpleui/README.md`](simpleui/README.md). SimpleUI añade `raylib-go` como única dependencia directa del módulo; las fuentes y sus licencias están incluidas.

### Espectro y notch automático

```go
bins := make([]float32, 129)
processor.SpectrumBeforeNotch(bins, 16_000)
if hz, ok := audio.DetectTone(bins, 16_000, 200, 6_000); ok {
    cfg.Notch.Enabled = true
    cfg.Notch.FrequencyHz = hz
    processor.Configure(cfg)
}
```

### Twin PBT

```go
pbt := audio.NewTwinPBT(iqSampleRate, 48_000)
pcm := pbt.Process(interleavedIQ, audio.USB, offsetHz, 100, 3250)
processor.Process(pcm)
```

`TwinPBT` espera pares `I,Q` normalizados. El resultado es PCM mono a la frecuencia de salida indicada. Si la entrada ya es PCM, no se debe usar PBT: use `LowCutHz` y `HighCutHz` del procesador.

## Consideraciones de integración

- Mantenga una instancia durante toda la transmisión; los filtros conservan estado entre bloques.
- Use bloques consecutivos sin solaparlos ni omitir muestras.
- `Process` añade 256 muestras de latencia cuando la reducción de ruido está activa, debido al FFT con overlap-add.
- La reducción de ruido aprende durante aproximadamente 0,5 s. Llame a `ResetNoiseLearning` al cambiar de canal o fuente.
- El espectro usa las últimas 512 muestras. Una actualización visual de 30–60 Hz es suficiente.
- `Configure`, `Process`, las consultas de espectro y estado están protegidos para poder llamarse desde hilos distintos.
- El ajuste que GO-Zero mostraba como 50/75 µs era desénfasis de recepción FM. Esta biblioteca expone además el preénfasis inverso para aplicaciones de transmisión/codificación.

## Verificación

```powershell
go test ./...
go vet ./...
```
