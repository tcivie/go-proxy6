# Contributing to IPv6 Proxy

Thank you for your interest in contributing to IPv6 Proxy! 🎉

## Getting Started

### Prerequisites
- Go 1.22 or later
- Docker (for testing)
- Basic understanding of networking and IPv6

### Development Setup

1. **Fork and clone the repository**
   ```bash
   git clone https://github.com/tcivie/go-proxy6.git
   cd go-proxy6
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Build and test**
   ```bash
   go build -o ipv6-proxy main.go
   go test -v ./...
   ```

## How to Contribute

### 🐛 Bug Reports
Found a bug? Please create an issue with:
- Clear description of the problem
- Steps to reproduce
- Expected vs actual behavior
- Your environment (OS, Go version, IPv6 setup)

### 💡 Feature Requests
Have an idea? Open an issue describing:
- The problem you're trying to solve
- Your proposed solution
- Why it would be useful

### 🔧 Code Contributions

1. **Create a feature branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**
   - Follow Go best practices
   - Add tests for new functionality
   - Update documentation if needed

3. **Test your changes**
   ```bash
   go test -v ./...
   go build -o ipv6-proxy main.go
   ```

4. **Commit and push**
   ```bash
   git add .
   git commit -m "feat: add your feature description"
   git push origin feature/your-feature-name
   ```

5. **Create a Pull Request**
   - Use a clear title and description
   - Link any related issues
   - Enable maintainer edits

## Code Guidelines

### Style
- Follow standard Go formatting (`go fmt`)
- Use meaningful variable and function names
- Add comments for complex logic
- Keep functions focused and small

### Testing
- Write tests for new features
- Ensure existing tests pass
- Test with real IPv6 subnets when possible

### Documentation
- Update README.md for user-facing changes
- Add inline comments for complex code
- Include examples in documentation

## Areas We Need Help With

- 🧪 **Testing**: More comprehensive test coverage
- 📚 **Documentation**: Better examples and guides
- 🐳 **Docker**: Optimization and multi-arch support
- 🔒 **Security**: Security reviews and improvements
- 🌐 **IPv6**: Support for different provider configurations
- ⚡ **Performance**: Optimization and benchmarking

## Questions?

- Create a [GitHub issue](https://github.com/tcivie/go-proxy6/issues) for questions
- Check existing issues and discussions first

## Code of Conduct

Be respectful and constructive in all interactions. We're all here to learn and improve the project together.

---

Thanks for contributing! 🚀
