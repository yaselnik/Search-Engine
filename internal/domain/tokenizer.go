package domain

/*
 * Defines the contract for functions that split text into tokens.
 */
type Tokenizer func(text string) []Token
