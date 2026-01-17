---
applyTo: "**/*_test.go,test/**/*.go"
---

# Test Code Instructions

## Testing Framework

- Use Ginkgo v2 for BDD-style testing
- Use Gomega for assertions and matchers
- Organize tests with `Describe`, `Context`, and `It` blocks
- Use `BeforeEach` and `AfterEach` for setup and teardown

## Test Structure

- Group related tests in `Describe` blocks
- Use `Context` to describe different scenarios
- Write descriptive `It` statements that read like specifications
- Keep test cases focused on a single behavior
- Use table-driven tests with `DescribeTable` for multiple similar cases

## Assertions

- Use Gomega matchers for clear and readable assertions
- Prefer `Expect(actual).To(matcher)` over `Expect(matcher).To(BeTrue())`
- Use `Eventually` for asynchronous assertions
- Use `Consistently` when testing that something remains stable
- Choose appropriate matchers: `Equal`, `BeNil`, `Succeed`, `MatchError`, etc.

## Mocking

- Generate mocks using mockgen (via `go generate`)
- Use go.uber.org/mock for mock generation and verification
- Set up mock expectations in `BeforeEach` when shared across tests
- Clear expectations with `EXPECT()` calls
- Verify all expectations are met

## Test Data

- Use meaningful test data that reflects real-world scenarios
- Create test fixtures in `testdata/` directories when appropriate
- Use builder patterns for complex test objects
- Keep test data minimal but sufficient to validate behavior

## Controller Testing

- Use controller-runtime's envtest for integration testing
- Set up test environment in `BeforeSuite`
- Clean up resources in `AfterEach`
- Test both successful reconciliation and error cases
- Verify status updates and conditions
- Test finalizer logic and cleanup

## Test Utilities

- Use shared test utilities from `internal/testing/`
- Create reusable test helpers for common operations
- Mock client operations consistently
- Handle test timeouts appropriately

## Best Practices

- Don't use dot imports except for Ginkgo and Gomega (staticcheck allows this)
- Make tests independent - they should not depend on execution order
- Clean up resources after tests
- Use descriptive names for test functions and variables
- Test edge cases and error conditions
- Avoid testing implementation details - focus on behavior
