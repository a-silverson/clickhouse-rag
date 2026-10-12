package schema

import "fmt"

type Column struct {
	Name     string
	Kind     string
	Required bool
}

type Table struct {
	Name    string
	ID      string
	Columns []Column
}

var Tables = []Table{
	{
		Name: "country",
		ID:   "country_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "code", Kind: "string", Required: true},
		},
	},
	{
		Name: "developer",
		ID:   "developer_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "country_id", Kind: "uuid", Required: true},
		},
	},
	{
		Name: "location",
		ID:   "location_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "country_id", Kind: "uuid", Required: true},
			{Name: "city", Kind: "string", Required: true},
		},
	},
	{
		Name: "project",
		ID:   "project_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "developer_id", Kind: "uuid", Required: true},
			{Name: "location_id", Kind: "uuid", Required: true},
			{Name: "country_id", Kind: "uuid", Required: true},
			{Name: "description", Kind: "string", Required: false},
		},
	},
	{
		Name: "apartments",
		ID:   "apartment_id",
		Columns: []Column{
			{Name: "project_id", Kind: "uuid", Required: true},
			{Name: "unit_code", Kind: "string", Required: true},
			{Name: "rooms", Kind: "uint8", Required: true},
			{Name: "area_sqm", Kind: "float", Required: true},
			{Name: "price_minor", Kind: "uint64", Required: true},
		},
	},
}

func Lookup(name string) (Table, error) {
	for _, table := range Tables {
		if table.Name == name {
			return table, nil
		}
	}
	return Table{}, fmt.Errorf("unknown table %q", name)
}

func Names() []string {
	out := make([]string, len(Tables))
	for i, table := range Tables {
		out[i] = table.Name
	}
	return out
}
