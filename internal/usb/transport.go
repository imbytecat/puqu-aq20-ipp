package usb

const (
	VendorID  = 0x8888
	ProductID = 0x0026
)

type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

type ConnectOptions struct {
	ID string
}
