# IC-RADIO v0.5.0

Nueva versión para Windows x64 centrada en la personalización completa del
audio y la recuperación rápida de configuraciones RX/TX.

## Novedades

- Nuevo modificador de voz TX ajustable entre -4 y +4 semitonos en pasos de
  medio semitono, sin cambiar la duración de la locución.
- Procesamiento granular continuo con compensación tonal para mantener el
  cuerpo de la voz en desplazamientos moderados.
- Panel de filtros TX dividido en dos módulos: paso de banda y modificador de
  voz.
- Tres presets globales capaces de guardar conjuntamente todos los parámetros
  DSP de RX y TX.
- Nombre editable para cada preset y confirmación antes de sobrescribir una
  ranura.
- Botones `LOAD` y `SAVE` para recuperar o actualizar rápidamente cada perfil.
- Los presets, sus nombres y el modificador de voz permanecen guardados al
  cerrar el programa.

## Descarga

Descarga `IC-RADIO-Windows-x64-v0.5.0.zip`, extrae el archivo y ejecuta
`ic-radio.exe`. El binario no está firmado digitalmente, por lo que Windows
puede solicitar confirmación la primera vez.

## Importante

La radio debe admitir audio USB y control PTT mediante RTS o DTR. Es necesario
seleccionar en el transceptor la entrada USB como fuente de modulación. Para
usar `MON`, se recomiendan auriculares con el fin de evitar realimentación.
