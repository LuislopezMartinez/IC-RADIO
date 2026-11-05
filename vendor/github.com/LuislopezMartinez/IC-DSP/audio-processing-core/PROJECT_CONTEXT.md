# Contexto para continuar el proyecto

## Origen y objetivo

Este módulo separa la cadena DSP que estaba acoplada a las pantallas y al receptor de GO-Zero. Su objetivo es servir como núcleo para un programa nuevo sin importar Raylib, la interfaz de GO-Zero, su sistema de traducciones ni su configuración.

También contiene una copia independiente de `simpleui` para construir la interfaz del programa nuevo. El núcleo DSP continúa sin usar Raylib; solamente el subpaquete `audio-processing-core/simpleui` depende de `raylib-go`.

La implementación original relevante estaba repartida entre `screens/audio_processor.go`, `screens/audio_noise_reducer.go`, `screens/audio_panel.go`, `internal/dsp/ssb.go` e `internal/dsp/nfm.go`. Aquí se han convertido los controles visuales en tipos y métodos públicos.

## Flujo de señal

PCM: paso alto/bajo → captura de espectro pre-notch → notch → reducción de ruido → EQ → énfasis/desénfasis → limitador → espectro de salida.

IQ SSB: Twin PBT USB/LSB → PCM mono → `Processor.Process`.

## Archivos

- `processor.go`: cadena PCM, sincronización y API principal.
- `filters.go`: EQ, notch y énfasis/desénfasis.
- `noise.go`: Wiener espectral adaptativo, FFT y overlap-add.
- `spectrum.go`: analizador y detección automática de tonos.
- `pbt.go`: selección Twin PBT y demodulación USB/LSB desde IQ.
- `types.go`: configuración y tipos públicos.
- `processor_test.go` y `example_test.go`: pruebas y ejemplo compilable.
- `simpleui/`: biblioteca retained-mode, fuentes embebidas, pruebas y documentación propia.
- `simpleui/internal/i18n/`: adaptador local que elimina la dependencia del catálogo de GO-Zero y permite registrar un callback de traducción.

## Decisiones importantes

- PCM e IQ usan `float32`; los cálculos internos sensibles usan `float64`.
- El módulo no realiza E/S: el programa anfitrión decide cómo capturar, reproducir o guardar audio.
- El reductor conserva el algoritmo original de 512 puntos y hop de 256 muestras, adaptando el límite de voz a la frecuencia de muestreo.
- El notch mantiene interpolación de coeficientes para evitar zipper noise al moverlo.
- El espectro entrega datos, no gráficos; así puede representarse con cualquier toolkit.
- El PBT se mantiene separado porque trabaja sobre IQ, mientras el resto opera sobre PCM.
- SimpleUI mantiene la API original, pero ahora puede importarse desde cualquier programa mediante `github.com/LuislopezMartinez/IC-DSP/audio-processing-core/simpleui`.
- Las fuentes Inter y JetBrains Mono y sus licencias OFL viajan dentro del subpaquete.

## Próximos pasos recomendados

1. Añadir adaptadores de entrada/salida apropiados para el programa nuevo (WAV, PortAudio, SDR, red, etc.).
2. Crear una capa de presets sin modificar el motor DSP.
3. Añadir benchmarks con los tamaños de bloque reales del nuevo programa.
4. Si se necesita estéreo, usar una instancia por canal o crear un envoltorio que mantenga estados separados.

## Límites conocidos

- Twin PBT es un demodulador mono sencillo y presupone una tasa IQ mayor o igual a la tasa PCM.
- El detector notch automático busca picos estrechos aislados; no pretende clasificar interferencias complejas.
- El preénfasis incluye una limitación preventiva de ganancia ultrasónica, pero el programa anfitrión debe conservar margen antes de codificar o transmitir.
