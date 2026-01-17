---
applyTo: "**/*.go"
---

# Go Code Instructions

## Code Style

- Use standard Go formatting (gofmt/goimports)
- Follow effective Go guidelines
- Use meaningful variable and function names
- Keep functions small and focused
- Prefer composition over inheritance
- Use interfaces for abstraction where appropriate

## Error Handling

- Always handle errors explicitly
- Wrap errors with context using `fmt.Errorf` with `%w` verb
- Return errors rather than panicking
- Use custom error types from `internal/client/errors.go` where appropriate
- Log errors with appropriate severity levels

## Imports

- Group imports in three sections: standard library, external packages, internal packages
- Use goimports to manage import formatting
- Avoid dot imports except for Ginkgo/Gomega in test files (allowed by staticcheck config)

## Concurrency

- Use channels for communication between goroutines
- Properly close channels when done
- Use context for cancellation and timeouts
- Avoid sharing memory; communicate by sharing

## Performance

- Preallocate slices when size is known
- Avoid unnecessary allocations in hot paths
- Use string builders for string concatenation
- Be mindful of copying large structs

## Comments

- Write package comments for all packages
- Document exported functions, types, and constants
- Use complete sentences in comments
- Start comments with the name of the element being described
- Include examples in doc comments for complex APIs

## Testing

- Name test files with `_test.go` suffix
- Use table-driven tests for multiple test cases
- Use Ginkgo's BDD-style testing with `Describe`, `Context`, `It`
- Use Gomega matchers for assertions
- Mock external dependencies using mockgen
- Test both happy paths and error cases
