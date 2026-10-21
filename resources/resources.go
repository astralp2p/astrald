package resources

// Resources reads and writes named files. Read and Write resolve a name against the config root;
// ReadData and WriteData resolve it against the data root.
type Resources interface {
	Read(name string) ([]byte, error)
	Write(name string, data []byte) error
	ReadData(name string) ([]byte, error)
	WriteData(name string, data []byte) error
}
