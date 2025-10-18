package main

import (
	"bufio"
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TReadBuffer
// ==============================================================================
type TReadBuffer struct {
	Buffer   []byte
	Position int
}

func (ReadBuffer *TReadBuffer) CanRead(Bytes int) bool {
	return ReadBuffer.Position+Bytes <= len(ReadBuffer.Buffer)
}

func (ReadBuffer *TReadBuffer) Overflowed() bool {
	return ReadBuffer.Position > len(ReadBuffer.Buffer)
}

func (ReadBuffer *TReadBuffer) ReadFlag() bool {
	return ReadBuffer.Read8() != 0
}

func (ReadBuffer *TReadBuffer) Read8() uint8 {
	Result := uint8(0)
	if ReadBuffer.CanRead(1) {
		Result = ReadBuffer.Buffer[ReadBuffer.Position]
	}
	ReadBuffer.Position += 1
	return Result
}

func (ReadBuffer *TReadBuffer) Read16() uint16 {
	Result := uint16(0)
	if ReadBuffer.CanRead(2) {
		Result = binary.LittleEndian.Uint16(ReadBuffer.Buffer[ReadBuffer.Position:])
	}
	ReadBuffer.Position += 2
	return Result
}

func (ReadBuffer *TReadBuffer) Read16BE() uint16 {
	Result := uint16(0)
	if ReadBuffer.CanRead(2) {
		Result = binary.BigEndian.Uint16(ReadBuffer.Buffer[ReadBuffer.Position:])
	}
	ReadBuffer.Position += 2
	return Result
}

func (ReadBuffer *TReadBuffer) Read32() uint32 {
	Result := uint32(0)
	if ReadBuffer.CanRead(4) {
		Result = binary.LittleEndian.Uint32(ReadBuffer.Buffer[ReadBuffer.Position:])
	}
	ReadBuffer.Position += 4
	return Result
}

func (ReadBuffer *TReadBuffer) Read32BE() uint32 {
	Result := uint32(0)
	if ReadBuffer.CanRead(4) {
		Result = binary.BigEndian.Uint32(ReadBuffer.Buffer[ReadBuffer.Position:])
	}
	ReadBuffer.Position += 4
	return Result
}

func (ReadBuffer *TReadBuffer) ReadString() string {
	Length := int(ReadBuffer.Read16())
	if Length == 0xFFFF {
		Length = int(ReadBuffer.Read32())
	}

	Result := ""
	if ReadBuffer.CanRead(Length) {
		// IMPORTANT(fusion): The game server uses LATIN1 encoding, which forces
		// the query manager to use LATIN1 encoding for text, at least on the
		// protocol level.
		Input := ReadBuffer.Buffer[ReadBuffer.Position:][:Length]
		Result = string(Latin1ToUTF8(Input))
	}
	ReadBuffer.Position += Length
	return Result
}

func (ReadBuffer *TReadBuffer) ReadBytes(Count int) []byte {
	Result := []byte(nil)
	if ReadBuffer.CanRead(Count) {
		Result = ReadBuffer.Buffer[ReadBuffer.Position:][:Count]
	}
	ReadBuffer.Position += Count
	return Result
}

// TWriteBuffer
// ==============================================================================
type TWriteBuffer struct {
	Buffer   []byte
	Position int
}

func (WriteBuffer *TWriteBuffer) CanWrite(Bytes int) bool {
	return (WriteBuffer.Position + Bytes) <= len(WriteBuffer.Buffer)
}

func (WriteBuffer *TWriteBuffer) Overflowed() bool {
	return WriteBuffer.Position > len(WriteBuffer.Buffer)
}

func (WriteBuffer *TWriteBuffer) WriteFlag(Value bool) {
	Value8 := uint8(0)
	if Value {
		Value8 = uint8(1)
	}
	WriteBuffer.Write8(Value8)
}

func (WriteBuffer *TWriteBuffer) Write8(Value uint8) {
	if WriteBuffer.CanWrite(1) {
		WriteBuffer.Buffer[WriteBuffer.Position] = Value
	}
	WriteBuffer.Position += 1
}

func (WriteBuffer *TWriteBuffer) Write16(Value uint16) {
	if WriteBuffer.CanWrite(2) {
		binary.LittleEndian.PutUint16(WriteBuffer.Buffer[WriteBuffer.Position:], Value)
	}
	WriteBuffer.Position += 2
}

func (WriteBuffer *TWriteBuffer) Write16BE(Value uint16) {
	if WriteBuffer.CanWrite(2) {
		binary.BigEndian.PutUint16(WriteBuffer.Buffer[WriteBuffer.Position:], Value)
	}
	WriteBuffer.Position += 2
}

func (WriteBuffer *TWriteBuffer) Write32(Value uint32) {
	if WriteBuffer.CanWrite(4) {
		binary.LittleEndian.PutUint32(WriteBuffer.Buffer[WriteBuffer.Position:], Value)
	}
	WriteBuffer.Position += 4
}

func (WriteBuffer *TWriteBuffer) Write32BE(Value uint32) {
	if WriteBuffer.CanWrite(4) {
		binary.BigEndian.PutUint32(WriteBuffer.Buffer[WriteBuffer.Position:], Value)
	}
	WriteBuffer.Position += 4
}

func (WriteBuffer *TWriteBuffer) WriteString(String string) {
	// IMPORTANT(fusion): The game server uses LATIN1 encoding, which forces
	// the query manager to use LATIN1 encoding for text, at least on the
	// protocol level.
	Output := UTF8ToLatin1([]byte(String))
	Length := len(Output)
	if Length < 0xFFFF {
		WriteBuffer.Write16(uint16(Length))
	} else {
		WriteBuffer.Write16(0xFFFF)
		WriteBuffer.Write32(uint32(Length))
	}

	if WriteBuffer.CanWrite(Length) {
		copy(WriteBuffer.Buffer[WriteBuffer.Position:], Output)
	}
	WriteBuffer.Position += Length
}

func (WriteBuffer *TWriteBuffer) WriteBytes(Bytes []byte) {
	Count := len(Bytes)
	if WriteBuffer.CanWrite(Count) {
		copy(WriteBuffer.Buffer[WriteBuffer.Position:], Bytes)
	}
	WriteBuffer.Position += Count
}

func (WriteBuffer *TWriteBuffer) Rewrite16(Position int, Value uint16) {
	if (Position+2) <= WriteBuffer.Position && !WriteBuffer.Overflowed() {
		binary.LittleEndian.PutUint16(WriteBuffer.Buffer[Position:], Value)
	}
}

func (WriteBuffer *TWriteBuffer) Insert32(Position int, Value uint32) {
	if Position <= WriteBuffer.Position {
		if WriteBuffer.CanWrite(4) {
			copy(WriteBuffer.Buffer[Position+4:], WriteBuffer.Buffer[Position:])
			binary.LittleEndian.PutUint32(WriteBuffer.Buffer[Position:], Value)
		}
		WriteBuffer.Position += 4
	}
}

// Config
// ==============================================================================
func ParseBoolean(String string) bool {
	return strings.EqualFold(String, "true")
}

func ParseInteger(String string) int {
	Result, Err := strconv.Atoi(String)
	if Err != nil {
		g_LogErr.Printf("Failed to parse integer \"%v\": %v", String, Err)
	}
	return Result
}

func ParseIntegerSuffix(String string) (int, string) {
	SuffixStart := -1
	for Index, Rune := range String {
		if !unicode.IsDigit(Rune) {
			SuffixStart = Index
			break
		}
	}

	var Value, Suffix string
	if SuffixStart != -1 {
		Value = strings.TrimSpace(String[:SuffixStart])
		Suffix = strings.TrimSpace(String[SuffixStart:])
	} else {
		Value = String
		Suffix = ""
	}

	Result, Err := strconv.Atoi(Value)
	if Err != nil {
		g_LogErr.Printf("Failed to parse duration \"%v\": %v", String, Err)
	}

	return Result, Suffix
}

func ParseDuration(String string) time.Duration {
	Value, Suffix := ParseIntegerSuffix(String)
	Result := time.Duration(Value) * time.Millisecond
	if len(Suffix) > 0 {
		switch Suffix[0] {
		case 'S', 's':
			Result = time.Duration(Value) * time.Second
		case 'M', 'm':
			Result = time.Duration(Value) * time.Minute
		case 'H', 'h':
			Result = time.Duration(Value) * time.Hour
		}
	}
	return Result
}

func ParseSize(String string) int {
	Result, Suffix := ParseIntegerSuffix(String)
	if len(Suffix) > 0 {
		switch Suffix[0] {
		case 'K', 'k':
			Result *= 1024
		case 'M', 'm':
			Result *= (1024 * 1024)
		}
	}
	return Result
}

func ParseString(String string) string {
	if len(String) > 2 {
		if String[0] == '"' && String[len(String)-1] == '"' ||
			String[0] == '\'' && String[len(String)-1] == '\'' ||
			String[0] == '`' && String[len(String)-1] == '`' {
			String = String[1 : len(String)-1]
		}
	}
	return String
}

func ReadConfig(FileName string, KVCallback func(string, string)) bool {
	File, Err := os.Open(FileName)
	if Err != nil {
		g_LogErr.Print(Err)
		return false
	}
	defer File.Close()

	Scanner := bufio.NewScanner(File)
	for LineNumber := 1; Scanner.Scan(); LineNumber += 1 {
		Line := strings.TrimSpace(Scanner.Text())
		if len(Line) == 0 || Line[0] == '#' {
			continue
		}

		Key, Value, Ok := strings.Cut(Scanner.Text(), "=")
		if !Ok {
			g_LogErr.Printf("%v:%v: No assignment found on non empty line", FileName, LineNumber)
			continue
		}

		Key = strings.TrimSpace(Key)
		if len(Key) == 0 {
			g_LogErr.Printf("%v:%v: Empty key", FileName, LineNumber)
			continue
		}

		Value = strings.TrimSpace(Value)
		if len(Value) == 0 {
			g_LogErr.Printf("%v:%v: Empty value", FileName, LineNumber)
			continue
		}

		KVCallback(Key, Value)
	}

	return true
}

// Utility
// ==============================================================================
func FileExists(FileName string) bool {
	_, Err := os.Stat(FileName)
	return Err == nil
}

func JoinHostPort(Host string, Port int) string {
	PortString := strconv.Itoa(Port)
	return net.JoinHostPort(Host, PortString)
}

func SplitDiscardEmpty(String string, Sep string) []string {
	var Result []string
	for String != "" {
		SubStr := ""
		Index := strings.Index(String, Sep)
		if Index == -1 {
			SubStr = String
			String = ""
		} else {
			SubStr = String[:Index]
			String = String[Index+1:]
		}

		if SubStr != "" {
			Result = append(Result, SubStr)
		}
	}
	return Result
}

func SwapAndPop[T any](Slice []T, Index int) []T {
	if len(Slice) == 0 {
		panic("slice is empty")
	}

	var Zero T
	Slice[Index] = Slice[len(Slice)-1]
	Slice[len(Slice)-1] = Zero
	return Slice[:len(Slice)-1]
}

func WorldTypeString(Type int) string {
	switch Type {
	case 0:
		return "Normal"
	case 1:
		return "Non-PvP"
	case 2:
		return "PvP-Enforced"
	default:
		return "Unknown"
	}
}

func FormatTimestamp(Timestamp int) string {
	String := "Never"
	if Timestamp > 0 {
		Time := time.Unix(int64(Timestamp), 0)
		String = Time.Format("Jan 02 2006, 15:04:05 MST")
	}
	return String
}

func FormatDurationSince(Timestamp int) string {
	String := "N/A"
	if Timestamp > 0{
		Duration := time.Since(time.Unix(int64(Timestamp), 0))
		String = Duration.Truncate(time.Second).String()
	}
	return String
}

func UTF8FindNextLeadingByte(Buffer []byte) int {
	Offset := 0
	for Offset < len(Buffer) {
		// NOTE(fusion): Allow the first byte to be a leading byte, in case we
		// just want to advance from one leading byte to another.
		if(Offset > 0 && utf8.RuneStart(Buffer[Offset])){
			break
		}
		Offset += 1
	}
	return Offset
}

func UTF8ToLatin1(Buffer []byte) []byte {
	ReadPos := 0
	Result := []byte{}
	for ReadPos < len(Buffer) {
		Codepoint, Size := utf8.DecodeRune(Buffer[ReadPos:])
		if Codepoint != utf8.RuneError {
			ReadPos += Size
		}else{
			ReadPos += UTF8FindNextLeadingByte(Buffer[ReadPos:])
		}

		if Codepoint >= 0 && Codepoint <= 0xFF {
			Result = append(Result, byte(Codepoint))
		}else{
			Result = append(Result, '?')
		}
	}
	return Result
}

func Latin1ToUTF8(Buffer []byte) []byte {
	Result := []byte{}
	for ReadPos := range Buffer {
		Result = utf8.AppendRune(Result, rune(Buffer[ReadPos]))
	}
	return Result
}
