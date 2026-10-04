// Package jsonparser repairs and parses partial JSON, such as the text of a
// structured output that is still streaming. FixJSON closes open strings,
// arrays and objects so that a half-finished document parses. ParsePartialJSON
// returns a ParseResult whose state says whether the input parsed as is, needed
// repair, or failed.
//
// Package ai uses it to deliver partial objects from ai.StreamText with an
// Output and from ai.StreamObject while the model is still writing.
package jsonparser
