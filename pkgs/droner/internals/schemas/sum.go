package schemas

type SumRequest struct {
	A int
	B int
}

type SumResponse struct {
	Sum int `json:"sum"`
}

func ValidateSumRequest(req SumRequest) error {
	return nil
}
