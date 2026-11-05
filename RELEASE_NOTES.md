# IC-RADIO v0.1.0

Primera versión pública para Windows x64.

## Incluye

- Consola combinada RX/TX para transceptores con audio USB.
- Procesamiento DSP independiente para recepción y transmisión.
- Filtros, ecualizador, notch RX, compresor TX y reducción de ruido.
- Énfasis SSB y deénfasis FM de 50/75 µs.
- Monitorización en auriculares de la señal TX procesada mediante `MON`.
- PTT mediante RTS o DTR, atajo configurable y retorno seguro a RX.
- Persistencia completa e independiente de los parámetros RX y TX.
- Selector desplegable de dispositivos y espectro en tiempo real.

## Descarga

Descarga `IC-RADIO-Windows-x64.zip`, extrae el archivo y ejecuta
`ic-radio.exe`. El binario no está firmado digitalmente, por lo que Windows
puede solicitar confirmación la primera vez.

## Importante

La radio debe admitir audio USB y control PTT mediante RTS o DTR. Es necesario
seleccionar en el transceptor la entrada USB como fuente de modulación. Para
usar `MON`, se recomiendan auriculares con el fin de evitar realimentación.
