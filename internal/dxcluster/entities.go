package dxcluster

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

//go:embed data/cty.csv
var countryFile string

//go:embed data/flags.json
var flagIndex []byte

//go:embed data/flags.zip
var flagArchive []byte

type Zone int

const (
	All Zone = iota
	NorthAmerica
	CentralAmerica
	SouthAmerica
	Europe
	Africa
	Asia
	Oceania
	Antarctica
)

var ZoneNames = [...]string{"ALL", "Norteamérica", "Centroamérica y Caribe", "Sudamérica", "Europa", "África", "Asia", "Oceanía", "Antártida"}

type Entity struct {
	Name, Flag string
	Zone       Zone
}

var exactCalls, prefixes = loadEntities()

func loadEntities() (map[string]Entity, map[string]Entity) {
	var index []struct {
		EntityCode  int
		CountryCode string
	}
	if err := json.Unmarshal(flagIndex, &index); err != nil {
		panic(err)
	}
	flags := make(map[int]string)
	for _, e := range index {
		flags[e.EntityCode] = strings.ToLower(e.CountryCode)
	}
	flags[29] = "ic"

	exact, prefix := make(map[string]Entity), make(map[string]Entity)
	reader := csv.NewReader(strings.NewReader(countryFile))
	reader.FieldsPerRecord = 10
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		id, err := strconv.Atoi(record[2])
		if err != nil {
			continue
		}
		entity := Entity{Name: record[1], Flag: flags[id], Zone: entityZone(record[3], id)}
		for _, alias := range strings.Fields(record[9]) {
			key := strings.ToUpper(strings.TrimRight(alias, ";"))
			if end := strings.IndexAny(key, "([<{~"); end >= 0 {
				key = key[:end]
			}
			if call, ok := strings.CutPrefix(key, "="); ok {
				exact[call] = entity
			} else if key != "" {
				prefix[key] = entity
			}
		}
	}
	return exact, prefix
}

func entityZone(continent string, id int) Zone {
	switch continent {
	case "NA":
		switch id {
		case 1, 6, 36, 50, 64, 204, 211, 237, 252, 277, 289, 291:
			return NorthAmerica
		default:
			return CentralAmerica
		}
	case "SA":
		return SouthAmerica
	case "EU":
		return Europe
	case "AF":
		return Africa
	case "AS":
		return Asia
	case "OC":
		return Oceania
	case "AN":
		return Antarctica
	}
	return All
}

func Lookup(call string) Entity {
	call = strings.ToUpper(strings.TrimSpace(call))
	if strings.HasSuffix(call, "/MM") || strings.HasSuffix(call, "/AM") {
		return Entity{}
	}
	if entity, ok := exactCalls[call]; ok {
		return entity
	}
	parts := strings.Split(call, "/")
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		switch last {
		case "P", "M", "QRP", "QRPP", "LH", "A":
			parts = parts[:len(parts)-1]
			continue
		}
		break
	}
	if entity, ok := exactCalls[strings.Join(parts, "/")]; ok {
		return entity
	}
	if len(parts) == 1 {
		return prefixEntity(parts[0])
	}
	if len(parts) == 2 {
		left, right := parts[0], parts[1]
		if len(right) == 1 && right[0] >= '0' && right[0] <= '9' {
			home := prefixEntity(left)
			if home != (Entity{}) {
				for i, c := range left {
					if c >= '0' && c <= '9' {
						if area := prefixEntity(left[:i] + right + left[i+1:]); area != (Entity{}) {
							return area
						}
						break
					}
				}
			}
			return home
		}
		leftEntity, rightEntity := prefixEntity(left), prefixEntity(right)
		if len(left) < len(right) && leftEntity != (Entity{}) {
			return leftEntity
		}
		if len(right) < len(left) && rightEntity != (Entity{}) {
			return rightEntity
		}
		if leftEntity == rightEntity {
			return leftEntity
		}
	}
	return Entity{}
}

func prefixEntity(call string) Entity {
	for i := len(call); i > 0; i-- {
		if entity, ok := prefixes[call[:i]]; ok {
			return entity
		}
	}
	return Entity{}
}

func Flags() map[string][]byte {
	flags := make(map[string][]byte)
	reader, err := zip.NewReader(bytes.NewReader(flagArchive), int64(len(flagArchive)))
	if err != nil {
		return flags
	}
	for _, file := range reader.File {
		data, err := fsReadFlag(reader, file.Name)
		if err == nil {
			flags[strings.TrimSuffix(file.Name, ".png")] = data
		}
	}
	return flags
}

func fsReadFlag(reader *zip.Reader, name string) ([]byte, error) {
	f, err := reader.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
