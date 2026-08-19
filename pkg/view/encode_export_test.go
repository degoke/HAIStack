package view

// NewRowEncoderForTest exposes RowEncoder for external tests.
func NewRowEncoderForTest() *RowEncoder {
	return newRowEncoder()
}
