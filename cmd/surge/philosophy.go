package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Principle represents a philosophical principle of Surge
type Principle struct {
	Title       string
	Subtitle    string
	Explanation string
}

var principles = []Principle{
	{
		Title:    "Explicit is better than implicit.",
		Subtitle: "Unless you enjoy debugging at 3 a.m.",
		Explanation: `Surge requires you to say what you mean. There are no implicit type
conversions, no hidden control flow, no silent coercions. If a value
moves, you see it. If it borrows, you see it. If it converts, you
write "to".

Example:
    let x: int = 42;
    let y: float = x to float;  // explicit conversion
    // let z: float = x;        // error: no implicit conversion

This principle runs through everything: mandatory return statements,
explicit ownership annotations (own, &, &mut), and visible generic
parameters. The extra keystrokes buy you clarity — and sleep.`,
	},
	{
		Title:    "Ownership without terror.",
		Subtitle: "Rust taught us the rules; we just skipped the nightmares.",
		Explanation: `Surge has Rust-level ownership semantics — own T, &T, &mut T — but
without lifetime annotations cluttering your function signatures.
Borrow scopes are lexical and obvious. When you need to end a borrow
early, there's @drop — not an incantation, just a statement.

Example:
    fn process(data: own Data) {
        let view = &data;       // borrow starts
        use_view(view);
        @drop view;             // borrow ends explicitly
        consume(data);          // now we can move data
    }

Memory safety doesn't have to feel like a fight with the compiler.
Surge keeps the guarantees and loses the anxiety.`,
	},
	{
		Title:    "The compiler is your ally, not your interrogator.",
		Subtitle: "It explains, not punishes.",
		Explanation: `When Surge finds a problem, it tells you what's wrong, where it is,
and often how to fix it. Diagnostics come with numeric codes, context,
and actionable hints. Tracing is built in — you can see every phase
of compilation if you want.

Example diagnostic:
    error[E0042]: cannot borrow 'data' as mutable
      --> src/main.sg:15:9
       |
    14 |     let view = &data;
       |                ----- immutable borrow here
    15 |     modify(&mut data);
       |            ^^^^^^^^^ mutable borrow here
       |
    hint: consider using @drop view; before line 15

The compiler wants to collaborate, not interrogate.`,
	},
	{
		Title:    "No magic — unless it wears a badge.",
		Subtitle: "Attributes are visible. Everything else is just code.",
		Explanation: `Surge has exactly one place for "compiler magic": attributes.
@entrypoint, @pure, @packed, @intrinsic — these are explicit
declarations of intent. They don't rewrite your code, they don't
perform hidden transformations. They're badges that say what's special.

Example:
    @entrypoint
    fn main() {
        // The badge is right there. No guessing.
    }

    @pure
    fn add(a: int, b: int) -> int {
        return a + b;  // compiler verifies no side effects
    }

Magic methods like __add and __to are equally visible — you can open
the stdlib and see their definitions. No hidden machinery.`,
	},
	{
		Title:    "Semantic honesty above all.",
		Subtitle: "You convert Egg to Bird, not Egg as Bird.",
		Explanation: `Surge names things for what they do. We use "to" for conversion
because you transform a value into another form — you don't pretend
it "is" something else. "compare" instead of "match" because we're
comparing shapes, not playing regex. "nothing" instead of "null"
because it means "absence of value", not "pointer to nowhere".

Example:
    let n: int = 42;
    let s: string = n to string;  // conversion, not casting

    compare result {
        Success(v) => handle(v);
        Error(e)   => report(e);
        finally    => panic("unexpected");
    }

Words matter. Honest names make honest code.`,
	},
	{
		Title:    "Data is data. Behavior is behavior.",
		Subtitle: "extern<T> keeps them neighbors, not roommates.",
		Explanation: `In Surge, types define what data looks like. Methods live nearby
in extern<T> blocks, but they're separate. This isn't arbitrary —
it keeps types clean and promotes clarity about what data "is"
versus what you can "do" with it.

Example:
    type Point = {
        x: float,
        y: float,
    };

    extern<Point> {
        fn distance(self: &Point, other: &Point) -> float {
            let dx = self.x - other.x;
            let dy = self.y - other.y;
            return (dx*dx + dy*dy).sqrt();
        }
    }

No inheritance hierarchies, no virtual dispatch confusion.
Just data and functions that work with it.`,
	},
	{
		Title:    "Tasks don't outlive their welcome.",
		Subtitle: "Structured concurrency: spawn, await, go home.",
		Explanation: `Async in Surge is predictable. Tasks cannot outlive their scope.
When an async block ends, it waits for all spawned tasks. No
fire-and-forget, no leaked goroutines, no zombie promises.

Example:
    async {
        let t1 = spawn fetch_data();
        let t2 = spawn process_cache();
        // ... do other work ...
    }  // implicit: waits for t1 and t2 here

Only own T crosses task boundaries — no borrowed references sneaking
between concurrent contexts. The result: Go-like simplicity with
Rust-like safety.`,
	},
	{
		Title:    "If it looks like it works, it works.",
		Subtitle: "If Surge forbids it, it tells you why — not \"go think.\"",
		Explanation: `Surge doesn't play guessing games. If your code compiles, it does
what it looks like it does. If Surge rejects it, you get an
explanation with context and suggestions. No cryptic template errors,
no "trait bounds not satisfied" without telling you which ones.

The goal is simple: you should be able to read Surge code and
understand it. No hidden rules, no implicit behaviors that change
meaning based on context you can't see. What you see is what runs.`,
	},
	{
		Title:    "Clarity beats cleverness.",
		Subtitle: "Future You will thank Present You.",
		Explanation: `Surge prefers a few extra characters if they make intent obvious.
Mandatory return statements, explicit type annotations where they
help, no operator overloading that hides complexity. The language
resists clever syntactic contortions.

Example:
    // Surge style
    fn calculate(x: int) -> int {
        let result = x * 2 + 1;
        return result;
    }

    // Not: implicit return, inferred everything
    // fn calculate(x) { x * 2 + 1 }

This isn't minimalism for its own sake. It's empathy for the reader —
including Future You at 2 a.m.`,
	},
	{
		Title:    "Simple enough to protect you. Strict enough to not terrify you.",
		Subtitle: "We found the middle path. It has snacks.",
		Explanation: `Most languages fall into two traps: too permissive (your mistakes
become silent bugs) or too strict (your mistakes become 37 compiler
errors). Surge aims for the middle: strict enough to catch real
problems, simple enough that the rules fit in your head.

No garbage collector, but ownership is boring and obvious. Strong
static typing, but inference where it helps. Borrow checking, but
lexical scopes you can see. The language should protect you without
making you feel like you're asking permission to code.`,
	},
	{
		Title:    "Surge is Surge.",
		Subtitle: "Lessons from Rust, Go, Python — but no costume parties.",
		Explanation: `Surge learns from great languages. Ownership clarity from Rust.
Toolchain simplicity from Go. Readable syntax from Python. But Surge
doesn't copy — it picks ideas because they serve you, not because
a committee decreed them.

That's why we have "compare" not "match", "Erring" not "Result",
"nothing" not "None". Same concepts, better fit for Surge's
philosophy. We're not here to win a cosplay contest. We're here
to be Surge.`,
	},
	{
		Title:    "The developer's sleep also matters.",
		Subtitle: "We build languages for humans who code, not for compilers who judge.",
		Explanation: `Surge is written by someone who knows what it's like to debug at
3 a.m., to fight a GC pause in the middle of a latency budget, to
wonder why a borrow checker chose violence today. The answer is:
be explicit, keep the rules small, keep the tone kind.

The compiler shouldn't make you feel stupid. Error messages should
help, not lecture. Documentation should explain, not gatekeep.
Because at the end of the day, languages are tools for humans.
And humans need sleep.`,
	},
}

var philosophyCmd = &cobra.Command{
	Use:   "philosophy",
	Short: "Display the philosophical principles of Surge",
	Long: `Display the philosophical principles that guide Surge's design.

Similar to Python's "Zen of Python", these principles explain
the core values and design decisions behind the language.

Examples:
  surge philosophy              # show all principles
  surge philosophy --explain 4  # explain principle 4 in detail
  surge philosophy -e 4         # same, short form
  surge philosophy --explain-all # show all with explanations`,
	Args: cobra.NoArgs,
	RunE: runPhilosophy,
}

func init() {
	philosophyCmd.Flags().IntP("explain", "e", 0, "explain principle N (1-12) in detail")
	philosophyCmd.Flags().Bool("explain-all", false, "show all principles with explanations")
}

func runPhilosophy(cmd *cobra.Command, _ []string) error {
	explain, err := cmd.Flags().GetInt("explain")
	if err != nil {
		return fmt.Errorf("failed to get explain flag: %w", err)
	}

	explainAll, err := cmd.Flags().GetBool("explain-all")
	if err != nil {
		return fmt.Errorf("failed to get explain-all flag: %w", err)
	}

	if explain > 0 {
		return printExplanation(explain)
	}

	if explainAll {
		return printAllExplanations()
	}

	return printPhilosophy()
}

func printPhilosophy() error {
	fmt.Println()
	fmt.Println("The Philosophy of Surge")
	fmt.Println(strings.Repeat("=", 24))
	fmt.Println()

	for i, p := range principles {
		fmt.Printf("%2d. %s\n", i+1, p.Title)
		fmt.Printf("    (%s)\n", p.Subtitle)
		fmt.Println()
	}

	fmt.Println("Use --explain N to learn more about a specific principle.")
	return nil
}

func printExplanation(n int) error {
	if n < 1 || n > len(principles) {
		return fmt.Errorf("principle number must be between 1 and %d, got %d", len(principles), n)
	}

	p := principles[n-1]

	fmt.Println()
	fmt.Printf("Principle %d: %s\n", n, p.Title)
	fmt.Printf("(%s)\n", p.Subtitle)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()
	fmt.Println(p.Explanation)
	fmt.Println()

	return nil
}

func printAllExplanations() error {
	fmt.Println()
	fmt.Println("The Philosophy of Surge — Complete Guide")
	fmt.Println(strings.Repeat("=", 42))

	for i, p := range principles {
		fmt.Println()
		fmt.Printf("Principle %d: %s\n", i+1, p.Title)
		fmt.Printf("(%s)\n", p.Subtitle)
		fmt.Println(strings.Repeat("-", 60))
		fmt.Println()
		fmt.Println(p.Explanation)
	}

	fmt.Println()
	return nil
}
