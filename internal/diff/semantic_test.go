package diff

import "testing"

func TestSemanticFingerprintIgnoresFormattingAndOrdinaryComments(t *testing.T) {
	tests := []struct {
		path string
		a    string
		b    string
	}{
		{"main.go", "package main\nfunc add(a,b int)int{return a+b}\n", "// explanation\npackage main\n\nfunc add(a, b int) int { return a + b }   \n"},
		{"index.ts", "const add=(a:number,b:number)=>a+b;\n", "// explanation\nconst add = (\n  a: number,\n  b: number,\n) => a + b;   \n"},
		{"component.tsx", "const view = <div>Hello world</div>\n", "// <span>ordinary comment whitespace</span>\nconst view = <div>Hello world</div>\n"},
		{"statements.js", "const a = 1\nconst b = 2\n", "const a = 1;\nconst b = 2;\n"},
		{"main.py", "def add(a, b):\n    return a + b\n", "# explanation\ndef add(a,b):   \n    return a+b  \n"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got, want := SemanticFingerprint(tt.path, tt.a), SemanticFingerprint(tt.path, tt.b); got != want {
				t.Fatalf("format/comment-only change altered fingerprint:\n%s\n%s", got, want)
			}
		})
	}
}

func TestSemanticFingerprintPreservesMeaningfulWhitespace(t *testing.T) {
	tests := []struct {
		name string
		path string
		a    string
		b    string
	}{
		{"string whitespace", "index.ts", `const x = "a b"`, `const x = "ab"`},
		{"python indent", "main.py", "if ready:\n    run()\n", "if ready:\nrun()\n"},
		{"javascript return newline", "index.js", "function f(){ return value }", "function f(){ return\nvalue }"},
		{"javascript array elision", "index.js", "const values = []", "const values = [,]"},
		{"javascript second array elision", "index.js", "const values = [,]", "const values = [,,]"},
		{"go inserted semicolon", "main.go", "package p\nfunc f() int { return 1 }\n", "package p\nfunc f() int { return\n1 }\n"},
		{"one character operator", "index.ts", "const ok = a == b", "const ok = a != b"},
		{"required empty loop body", "index.js", "function f(){ while (ready); }", "function f(){ while (ready) }"},
		{"required empty else body", "index.js", "function f(){ if (ready) run(); else; }", "function f(){ if (ready) run(); else }"},
		{"arrow restricted newline", "index.ts", "const f = (x) => x", "const f = (x)\n=> x"},
		{"identifier arrow restricted newline", "index.ts", "const f = x => x", "const f = x\n=> x"},
		{"jsx text whitespace", "index.jsx", "const x = <div><span>A</span> <span>B</span></div>", "const x = <div><span>A</span><span>B</span></div>"},
		{"tsx text whitespace", "index.tsx", "const x = <div>Hello world</div>", "const x = <div>Helloworld</div>"},
		{"jsx text after regex brace", "index.jsx", "const x=<div>{/{/.test(s)}<span>A</span> <span>B</span></div>", "const x=<div>{/{/.test(s)}<span>A</span><span>B</span></div>"},
		{"jsx text nested in expression", "index.tsx", "const x=<div>{ok && <><span>A</span> <span>B</span></>}</div>", "const x=<div>{ok && <><span>A</span><span>B</span></>}</div>"},
		{"jsx text after typescript generic", "index.tsx", "const id=<T>(x:T)=>x; const v=<div><span>A</span> <span>B</span></div>", "const id=<T>(x:T)=>x; const v=<div><span>A</span><span>B</span></div>"},
		{"rest parameter trailing comma", "index.ts", "function f(...args){}", "function f(...args,){}"},
		{"rest array trailing comma", "index.ts", "const values = [...args]", "const values = [...args,]"},
		{"rest object trailing comma", "index.ts", "const value = {...rest}", "const value = {...rest,}"},
		{"cgo preamble", "main.go", "package p\n/*\nint answer = 1;\n*/\nimport \"C\"\n", "package p\n/*\nint answer = 2;\n*/\nimport \"C\"\n"},
		{"rust doc attribute", "lib.rs", "#![deny(missing_docs)]\n/// docs\npub fn run() {}\n", "#![deny(missing_docs)]\npub fn run() {}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if SemanticFingerprint(tt.path, tt.a) == SemanticFingerprint(tt.path, tt.b) {
				t.Fatal("meaningful change produced identical fingerprint")
			}
		})
	}
}

func TestSemanticFingerprintDoesNotTreatTypeScriptGenericAsJSXText(t *testing.T) {
	a := "const id = <T>(x: T) => x\n"
	b := "const id=<T>( x:T )=>x\n"
	if SemanticFingerprint("index.tsx", a) != SemanticFingerprint("index.tsx", b) {
		t.Fatal("formatter-only generic arrow change altered TSX fingerprint")
	}
}

func TestSemanticFingerprintIgnoresOnlyRealArrayTrailingComma(t *testing.T) {
	withoutTrailing := `const values = [first, second]`
	withTrailing := `const values = [first, second,]`
	if SemanticFingerprint("index.ts", withoutTrailing) != SemanticFingerprint("index.ts", withTrailing) {
		t.Fatal("ordinary array trailing comma altered fingerprint")
	}
}

func TestSemanticFingerprintPreservesDirectives(t *testing.T) {
	tests := []struct {
		path string
		a    string
		b    string
	}{
		{"main.go", "//go:build linux\npackage p\n", "//go:build darwin\npackage p\n"},
		{"index.ts", "// @ts-ignore\ncall()\n", "call()\n"},
		{"types.d.ts", "/// <reference types=\"node\" />\nexport {}\n", "export {}\n"},
		{"main.py", "# type: ignore\ncall()\n", "call()\n"},
	}
	for _, tt := range tests {
		if SemanticFingerprint(tt.path, tt.a) == SemanticFingerprint(tt.path, tt.b) {
			t.Errorf("directive change for %s was ignored", tt.path)
		}
	}
}

func TestSemanticFingerprintPreservesOpaqueLiteralContents(t *testing.T) {
	tests := []struct {
		name string
		path string
		a    string
		b    string
	}{
		{"javascript regex", "index.js", `const url = /https?:\/\/a b/`, `const url = /https?:\/\/ab/`},
		{"rust raw string", "main.rs", `let x = r#"a /* not a comment */ b"#;`, `let x = r#"a /* not a comment */b"#;`},
		{"solidity string", "Main.sol", `string x = "a // text b";`, `string x = "a // textb";`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if SemanticFingerprint(tt.path, tt.a) == SemanticFingerprint(tt.path, tt.b) {
				t.Fatal("literal content change produced identical fingerprint")
			}
		})
	}
}

func TestJavaScriptRegexIsOneOpaqueToken(t *testing.T) {
	source := `const url = /https?:\/\/a b/i`
	tokens := lexSemantic(source, "js")
	for _, tok := range tokens {
		if tok.kind == "string" && tok.text == `/https?:\/\/a b/i` {
			return
		}
	}
	t.Fatalf("regex was not preserved as one token: %#v", tokens)
}

func TestJavaScriptRegexAfterControlParenRemainsOpaque(t *testing.T) {
	a := `if (ready) /a b/.test(value)`
	b := `if (ready) /a  b/.test(value)`
	if SemanticFingerprint("index.js", a) == SemanticFingerprint("index.js", b) {
		t.Fatal("regex whitespace after a control-flow parenthesis was ignored")
	}
}
