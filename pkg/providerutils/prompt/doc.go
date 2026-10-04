// Package prompt converts SDK messages into the shapes providers expect:
// extracting the system message, merging consecutive tool messages,
// normalizing file and image content, downloading files a provider cannot
// fetch itself, and mapping messages to the OpenAI, Anthropic and Google
// formats. Provider authors use it. Application code rarely needs it.
package prompt
