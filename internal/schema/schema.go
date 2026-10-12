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
		Name: "microservice",
		ID:   "microservice_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "description", Kind: "string", Required: false},
		},
	},
	{
		Name: "api",
		ID:   "api_id",
		Columns: []Column{
			{Name: "microservice_id", Kind: "uuid", Required: true},
			{Name: "name", Kind: "string", Required: true},
			{Name: "description", Kind: "string", Required: false},
		},
	},
	{
		Name: "pulsar_topic",
		ID:   "topic_id",
		Columns: []Column{
			{Name: "name", Kind: "string", Required: true},
			{Name: "description", Kind: "string", Required: false},
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
