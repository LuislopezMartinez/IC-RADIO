# IC-RADIO v0.3.0

Nueva versión para Windows x64 centrada en la seguridad del audio, el control
de transmisión y una interfaz más clara y precisa.

## Novedades

- El programa ya no modifica ni fuerza al 100 % el volumen de salida de
  Windows al iniciarse, recompilarse o cambiar de dispositivo.
- Nuevo interruptor `ON TOP` para mantener la consola siempre en primer plano.
- Espectro rediseñado desde 100 Hz, con más barras, mayor definición, mejor
  lectura de agudos y estética cibernética en cian, verde y ámbar.
- Ecualizador gráfico renovado, con mejor contraste, escala contenida dentro
  del panel y respuesta de agudos mejorada.
- Nueva distribución de la pestaña Filtros TX.
- Generador de tono TX ajustable entre 0 y 5 kHz.
- Nuevo `FADE ON PTT` configurable entre 0 y 3000 ms para abrir el audio de
  forma progresiva al comenzar la transmisión.
- Controles de paso de banda, presencia TX y estados activos reorganizados
  para una lectura más rápida.
- Paneles, sliders y estados activos con mayor contraste sobre el acabado de
  aluminio.
- Script `compilar-radio.cmd` ejecutable con doble clic desde el Explorador de
  Windows.

## Descarga

Descarga `IC-RADIO-Windows-x64-v0.3.0.zip`, extrae el archivo y ejecuta
`ic-radio.exe`. El binario no está firmado digitalmente, por lo que Windows
puede solicitar confirmación la primera vez.

## Importante

La radio debe admitir audio USB y control PTT mediante RTS o DTR. Es necesario
seleccionar en el transceptor la entrada USB como fuente de modulación. Para
usar `MON`, se recomiendan auriculares con el fin de evitar realimentación.
