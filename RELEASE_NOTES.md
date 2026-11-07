# IC-RADIO v0.4.0

Nueva versión para Windows x64 centrada en un control de transmisión más
directo, una distribución TX más compacta y un monitor de audio más robusto.

## Novedades

- Nuevo control de ganancia TX permanente bajo el PTT, disponible desde todas
  las pestañas de transmisión.
- PTT más compacto para separar claramente la transmisión del ajuste de nivel.
- La ganancia TX conserva su valor y puede ajustarse sin abandonar la sección
  DSP que se esté utilizando.
- Panel de eco TX redistribuido para aprovechar mejor el espacio disponible.
- El monitor `MON` vuelve a enumerar las salidas de audio antes de abrirlas,
  permitiendo reconocer cambios recientes en el dispositivo de Windows.
- Ecualizador documentado correctamente como 11 bandas hasta 10 kHz, con
  restablecimiento y bypass.

## Descarga

Descarga `IC-RADIO-Windows-x64-v0.4.0.zip`, extrae el archivo y ejecuta
`ic-radio.exe`. El binario no está firmado digitalmente, por lo que Windows
puede solicitar confirmación la primera vez.

## Importante

La radio debe admitir audio USB y control PTT mediante RTS o DTR. Es necesario
seleccionar en el transceptor la entrada USB como fuente de modulación. Para
usar `MON`, se recomiendan auriculares con el fin de evitar realimentación.
