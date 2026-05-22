package vertexai

type VertexaiErrors []*VertexaiError

type VertexaiError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func (e *VertexaiErrors) Error() *VertexaiError {
	return (*e)[0]
}
