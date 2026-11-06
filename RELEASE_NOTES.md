# IC-RADIO v0.2.0

Actualización principal para Windows x64 centrada en el procesamiento TX, la
legibilidad y una presentación más cercana a un procesador físico de rack.

## Novedades

- Nueva interfaz de aluminio cepillado inspirada en procesadores enracables.
- Selectores de audio desplegables con etiquetas y dispositivo claramente separados.
- Eco TX con retardo, realimentación, mezcla, perfiles rápidos y tonos oscuro,
  neutro y brillante ahora perceptiblemente diferenciados para audio de radio.
- Botón de bypass y restablecimiento independiente del ecualizador.
- Pestañas y botones activos con iluminación ámbar y mayor profundidad visual.
- Medidores con gradiente continuo y monitor `MON` para la señal TX procesada.
- Interfaz combinada RX/TX, notch RX gráfico, énfasis SSB y deénfasis FM.
- Persistencia completa e independiente de los parámetros RX y TX.
- Corrección del proceso de compilación para garantizar la detección de los
  dispositivos de audio reales mediante CGO/Miniaudio.

## Descarga

Descarga `IC-RADIO-Windows-x64.zip`, extrae el archivo y ejecuta
`ic-radio.exe`. El binario no está firmado digitalmente, por lo que Windows
puede solicitar confirmación la primera vez.

## Importante

La radio debe admitir audio USB y control PTT mediante RTS o DTR. Es necesario
seleccionar en el transceptor la entrada USB como fuente de modulación. Para
usar `MON`, se recomiendan auriculares con el fin de evitar realimentación.
