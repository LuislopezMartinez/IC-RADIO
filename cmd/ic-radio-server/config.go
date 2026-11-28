package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
)

type configuration struct {
	COM       string `json:"com"`
	CIV       string `json:"civ"`
	Port      string `json:"port"`
	Password  string `json:"password_verifier,omitempty"`
	RXAudioID string `json:"rx_audio_id,omitempty"`
	TXAudioID string `json:"tx_audio_id,omitempty"`
}

func passwordVerifier(password string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := sha256.Sum256(append(salt[:], password...))
	return hex.EncodeToString(salt[:]) + hex.EncodeToString(hash[:]), nil
}

func passwordCheck(verifier string) func(string) bool {
	data, err := hex.DecodeString(verifier)
	if err != nil || len(data) != 16+sha256.Size {
		return nil
	}
	salt, expected := data[:16:16], data[16:]
	return func(password string) bool {
		if !validPassword(password) {
			return false
		}
		hash := sha256.Sum256(append(salt, password...))
		return subtle.ConstantTimeCompare(hash[:], expected) == 1
	}
}

func validPassword(password string) bool {
	return len(password) >= 8 && len(password) <= 128 && strings.TrimSpace(password) == password
}

func promptPassword(reader *bufio.Scanner, out io.Writer, saved string) (string, error) {
	keep := passwordCheck(saved) != nil
	fmt.Fprintln(out, "\nContraseña de acceso")
	fmt.Fprintln(out, "   Escribe la misma contraseña en el servidor y en CFG de IC-RADIO.")
	fmt.Fprintln(out, "   Usa de 8 a 128 caracteres, sin espacios al principio ni al final.")
	if keep {
		fmt.Fprintln(out, "   Pulsa Enter para conservar la contraseña guardada.")
	}
	for {
		fmt.Fprint(out, "Contraseña: ")
		restore, hidden := hideConsoleInput()
		read := reader.Scan()
		restore()
		if hidden {
			fmt.Fprintln(out)
		}
		if !read {
			if err := reader.Err(); err != nil {
				return "", err
			}
			return "", errors.New("configuración cancelada")
		}
		password := reader.Text()
		if password == "" && keep {
			return saved, nil
		}
		if validPassword(password) {
			return passwordVerifier(password)
		}
		fmt.Fprintln(out, "Introduce una contraseña válida. Debe tener al menos 8 caracteres.")
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "IC-RADIO", "server.json"), nil
}

func loadConfiguration() configuration {
	defaults := configuration{COM: "COM3", CIV: "94", Port: remote.DefaultPort}
	path, err := configPath()
	if err != nil {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var saved configuration
	if json.Unmarshal(data, &saved) != nil || saved.COM == "" || !validCIV(saved.CIV) || !validPort(saved.Port) {
		return defaults
	}
	return saved
}

func saveConfiguration(cfg configuration) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "server-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(cfg)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func validCIV(value string) bool {
	number, err := strconv.ParseUint(value, 16, 8)
	return err == nil && number > 0 && number < 0xe0
}

func validPort(value string) bool {
	number, err := strconv.ParseUint(value, 10, 16)
	return err == nil && number > 0
}

func promptConfiguration(reader *bufio.Scanner, out io.Writer, ports []ptt.Port, defaults configuration) (configuration, error) {
	fmt.Fprintln(out, "Este servidor conecta tu emisora por USB con el programa IC-RADIO.")
	fmt.Fprintln(out, "Pulsa Enter sin escribir nada para aceptar el valor que aparece entre [corchetes].")
	fmt.Fprintln(out, "Después te pediremos la contraseña para conectar IC-RADIO.")
	fmt.Fprintln(out)
	if len(ports) == 0 {
		return configuration{}, errors.New("no se han encontrado puertos COM. Comprueba el cable USB de la emisora y vuelve a abrir el servidor")
	}
	fmt.Fprintln(out, "1 de 3. Elige el puerto USB de la emisora")
	fmt.Fprintln(out, "   Puedes escribir el número de la lista o el nombre del puerto.")
	defaultIndex := 0
	for i, port := range ports {
		if port.IsUSB {
			defaultIndex = i
			break
		}
	}
	for i, port := range ports {
		fmt.Fprintf(out, "  %d. %s", i+1, port.Name)
		if port.Description != "" && port.Description != port.Name {
			fmt.Fprintf(out, " · %s", port.Description)
		} else if port.IsUSB {
			fmt.Fprint(out, " · USB")
		}
		fmt.Fprintln(out)
		if strings.EqualFold(port.Name, defaults.COM) {
			defaultIndex = i
		}
	}
	selected := defaultIndex
	_, err := prompt(reader, out, "Puerto COM", ports[defaultIndex].Name, func(value string) bool {
		if number, err := strconv.Atoi(value); err == nil && number > 0 && number <= len(ports) {
			selected = number - 1
			return true
		}
		for i, port := range ports {
			if strings.EqualFold(value, port.Name) {
				selected = i
				return true
			}
		}
		return false
	})
	if err != nil {
		return configuration{}, err
	}
	fmt.Fprintln(out, "\n2 de 3. Confirma la dirección CI-V de la emisora")
	fmt.Fprintln(out, "   Es el código que identifica la emisora. Si lo cambias, usa el código de su menú sin la letra h final.")
	address, err := prompt(reader, out, "Dirección CI-V", defaults.CIV, validCIV)
	if err != nil {
		return configuration{}, err
	}
	fmt.Fprintln(out, "\n3 de 3. Confirma el puerto de conexión de IC-RADIO")
	fmt.Fprintln(out, "   Es el número que debes poner también en la pestaña CFG de IC-RADIO.")
	port, err := prompt(reader, out, "Puerto de conexión (1–65535)", defaults.Port, validPort)
	if err != nil {
		return configuration{}, err
	}
	defaults.COM, defaults.CIV, defaults.Port = ports[selected].Name, strings.ToUpper(address), port
	return defaults, nil
}

func prompt(reader *bufio.Scanner, out io.Writer, label, defaultValue string, valid func(string) bool) (string, error) {
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return "", err
			}
			return "", errors.New("configuración cancelada")
		}
		value := strings.TrimSpace(reader.Text())
		if value == "" {
			value = defaultValue
		}
		if valid(value) {
			return value, nil
		}
		fmt.Fprintln(out, "Ese valor no es válido. Escribe otro o pulsa Enter para aceptar el valor entre corchetes.")
	}
}
