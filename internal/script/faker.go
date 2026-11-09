package script

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/gi-runner/gi/internal/value"
	"github.com/grafana/sobek"
)

var (
	firstNames = []string{"James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael", "Linda", "David", "Elizabeth", "Wei", "Aisha", "Carlos", "Yuki", "Priya", "Omar"}
	lastNames  = []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Martinez", "Lee", "Kim", "Patel", "Nguyen", "Khan", "Silva", "Müller"}
	cities     = []string{"New York", "London", "Tokyo", "Paris", "Berlin", "Sydney", "Toronto", "Mumbai", "Singapore", "Dubai", "São Paulo", "Seoul"}
	countries  = []string{"United States", "United Kingdom", "Japan", "France", "Germany", "Australia", "Canada", "India", "Singapore", "Brazil", "Korea", "Mexico"}
	streets    = []string{"Main St", "Oak Ave", "Maple Dr", "Cedar Ln", "Pine Rd", "Elm St", "Park Ave", "Lake Blvd"}
	words      = []string{"alpha", "bravo", "cloud", "delta", "echo", "field", "grove", "harbor", "island", "jungle", "kernel", "lemon", "meadow", "nova", "orbit", "prism", "quartz", "river", "stone", "tiger"}
	companies  = []string{"Acme", "Globex", "Initech", "Umbrella", "Stark", "Wayne", "Wonka", "Hooli", "Vandelay", "Cyberdyne"}
	suffixes   = []string{"Inc", "LLC", "Corp", "Group", "Labs", "Systems"}
	jobs       = []string{"Engineer", "Designer", "Manager", "Analyst", "Consultant", "Developer", "Architect", "Director", "Specialist", "Coordinator"}
	domains    = []string{"example.com", "test.org", "mail.test", "demo.net"}
)

const alphanumerics = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randN(n int) int { return rand.IntN(n) } //nolint:gosec // fake test data

func randFloat() float64 { return rand.Float64() } //nolint:gosec // fake test data

func pick(list []string) string { return list[randN(len(list))] }

func between(args []any, lo, hi float64) (float64, float64) {
	if len(args) > 0 {
		if n, ok := value.AsFloat(args[0]); ok {
			lo = n
		}
	}
	if len(args) > 1 {
		if n, ok := value.AsFloat(args[1]); ok {
			hi = n
		}
	}
	return lo, hi
}

func (v *VM) faker() map[string]any {
	fullName := func() string { return pick(firstNames) + " " + pick(lastNames) }
	user := func() string {
		return strings.ToLower(pick(firstNames)) + "." + strings.ToLower(strings.ReplaceAll(pick(lastNames), "ü", "u")) + strconv.Itoa(randN(100))
	}
	sentence := func() string {
		n := 4 + randN(6)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = pick(words)
		}
		s := strings.Join(parts, " ")
		return strings.ToUpper(s[:1]) + s[1:] + "."
	}
	fns := map[string]func(args []any) any{
		"timestamp":    func([]any) any { return time.Now().Unix() },
		"timestampMs":  func([]any) any { return time.Now().UnixMilli() },
		"isoTimestamp": func([]any) any { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") },
		"firstName":    func([]any) any { return pick(firstNames) },
		"lastName":     func([]any) any { return pick(lastNames) },
		"fullName":     func([]any) any { return fullName() },
		"email":        func([]any) any { return user() + "@" + pick(domains) },
		"userName":     func([]any) any { return user() },
		"phoneNumber": func([]any) any {
			return fmt.Sprintf("(%03d) %03d-%04d", 200+randN(800), randN(1000), randN(10000))
		},
		"city":          func([]any) any { return pick(cities) },
		"country":       func([]any) any { return pick(countries) },
		"streetAddress": func([]any) any { return fmt.Sprintf("%d %s", 1+randN(9999), pick(streets)) },
		"zipCode":       func([]any) any { return fmt.Sprintf("%05d", randN(100000)) },
		"latitude":      func([]any) any { return randFloat()*180 - 90 },
		"longitude":     func([]any) any { return randFloat()*360 - 180 },
		"randomInt": func(args []any) any {
			lo, hi := between(args, 0, 1000)
			if hi < lo {
				lo, hi = hi, lo
			}
			return int64(lo) + int64(randN(int(hi)-int(lo)+1))
		},
		"randomFloat": func(args []any) any {
			lo, hi := between(args, 0, 1)
			return lo + randFloat()*(hi-lo)
		},
		"randomBoolean": func([]any) any { return randN(2) == 1 },
		"word":          func([]any) any { return pick(words) },
		"sentence":      func([]any) any { return sentence() },
		"paragraph": func([]any) any {
			n := 3 + randN(3)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = sentence()
			}
			return strings.Join(parts, " ")
		},
		"alphanumeric": func(args []any) any {
			n, _ := between(args, 10, 0)
			b := make([]byte, max(int(n), 0))
			for i := range b {
				b[i] = alphanumerics[randN(len(alphanumerics))]
			}
			return string(b)
		},
		"hexColor":    func([]any) any { return fmt.Sprintf("#%06x", randN(0x1000000)) },
		"companyName": func([]any) any { return pick(companies) + " " + pick(suffixes) },
		"jobTitle":    func([]any) any { return pick(jobs) },
		"creditCardNumber": func([]any) any {
			return fmt.Sprintf("4%03d %04d %04d %04d", randN(1000), randN(10000), randN(10000), randN(10000))
		},
	}
	out := make(map[string]any, len(fns))
	for name, fn := range fns {
		out[name] = func(call sobek.FunctionCall) sobek.Value {
			return v.ToValue(fn(exportArgs(call.Arguments, nil)))
		}
	}
	return out
}
