package csvtogo

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

var _defaultOps = Options{
	SkipHeader: true,
	Comma:      ',',
}

var csvCommaIsRequired = errors.New("Options.Comma is required")

type CsvToStruct[T any] struct {
	targetCSV string
	outsChan  chan []T
	outChan   chan T
	endChan   chan bool
	nextChan  chan bool
	errChan   chan error
	run       bool
	ops       Options
}

type Options struct {
	ChunkSize   int
	SkipHeader  bool
	SkipCol     []int
	ReplaceWith map[string]string
	Comma       rune
	skipper     map[int]int
}

func (c *CsvToStruct[T]) Rows() *CsvToStruct[T] {
	go func() {
		err := csvReader[T](
			c.targetCSV,
			c.ops.Comma,
			c.valueSetter,
		)
		if err != nil {
			c.errChan <- err
			return
		}
		c.endChan <- true
	}()
	return c
}

func (c *CsvToStruct[T]) ToList() *CsvToStruct[T] {
	go c.start()
	return c
}

func (c *CsvToStruct[T]) Next() bool {
	if c.run {
		c.nextChan <- true
		return true
	}
	return false
}

func (c *CsvToStruct[T]) Read() (*T, error) {
	for c.run {
		select {
		case data := <-c.outChan:
			return &data, nil
		case <-c.endChan:
			c.run = false
			return nil, io.EOF
		case err := <-c.errChan:
			c.run = false
			return nil, err
		default:
			//waiting
		}
	}
	return nil, nil
}

func (c *CsvToStruct[T]) start() {
	data := [][]string{
		{"AAAA0", "BBBB0", "111"},
		{"CCCC1", "DDDD1", "222"},
		{"CCCC2", "DDDD2", "333"},
		{"CCCC3", "DDDD3", "444"},
		{"CCCC4", "DDDD4", "555"},
		{"CCCC5", "DDDD5", "666"},
		{"CCCC6", "DDDD6", "999"},
	}
	ref := make([]T, 1)
	var out []T
	var isErr bool
	for _, val := range data {
		if isErr {
			return
		}
		tmp := ref[0] //copy reference
		err := c.setValue(val, &tmp)
		if err != nil {
			c.errChan <- err
		}
		out = append(out, tmp)
		c.sendChunk(&out)
	}

	//last chunk
	c.sendChunk(&out, len(out) > 0)
	c.endChan <- true
}

func (c *CsvToStruct[T]) setValue(data []string, tmp *T) error {
	col := 0
	for i, val := range data {
		//check if in target skipper
		if _, ok := c.ops.skipper[i]; ok {
			continue
		}
		f := reflect.ValueOf(tmp).Elem().Field(col)
		err := typeSafe(f, val)
		if err != nil {
			return err
		}
		col += 1
	}
	return nil
}

func (c *CsvToStruct[T]) sendChunk(out *[]T, force ...bool) {
	if len(*out) >= c.ops.ChunkSize || (len(force) > 0 && force[0]) {
		c.outsChan <- *out
		<-c.nextChan //w8 until client is ready to move
		*out = []T{}
	}
}

func (c *CsvToStruct[T]) send(out *T) {
	c.outChan <- *out
	<-c.nextChan //w8 until client is ready to move
}

func typeSafe(f reflect.Value, val string) error {
	switch f.Interface().(type) {
	case int, int32, int64:
		v, err := strconv.Atoi(val)
		if err != nil {
			fmt.Println("invalid csv type, recording to struct it should be type int") //TODO fixme, generate manual err
			return err
		}
		f.SetInt(int64(v))
	case string:
		f.SetString(val)
	case bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			fmt.Println("invalid bool type, recording to struct it should be type bool") //TODO fixme
			return err
		}
		f.SetBool(b)
	case float32, float64:
		v, err := strconv.ParseFloat(val, 64)
		if err != nil {
			fmt.Println("invalid bool type, recording to struct it should be type bool") //TODO fixme
			return err
		}
		f.SetFloat(v)
	default:
		return fmt.Errorf("csvtogo is not support type %v", f.Type().String())
	}
	return nil
}

func (c *CsvToStruct[T]) Close() {
	close(c.nextChan)
	close(c.outChan)
	close(c.endChan)
	close(c.errChan)
}

func (c *CsvToStruct[T]) isValidStruct(size int, fieldSize int) bool {
	return size == (fieldSize + len(c.ops.SkipCol))
}

func getRealNoOfCol(noOfCal int, skip int) int {
	if noOfCal < skip {
		return noOfCal
	}
	return noOfCal - skip
}

func (c *CsvToStruct[T]) valueSetter(ref T, data []string, counter int) error {
	//skip header if required
	if c.ops.SkipHeader && counter == 0 {
		return nil
	}

	v := reflect.ValueOf(&ref).Elem()
	if !c.isValidStruct(len(data), v.NumField()) {
		return fmt.Errorf("number of column is not match with struct at row: %v, expected: %v, got: %v", counter, v.NumField(), getRealNoOfCol(len(data), len(c.ops.SkipCol)))
	}
	err := c.setValue(data, &ref)
	if err != nil {
		return err
	}
	c.send(&ref)
	return nil
}