# D2 mutable index-receiver investigation

Investigated 2026-10-09. Published base: `145b0c8b7535f4eb2eb9970cf72392b84512ccfc`.
Rejected local candidate: `72f1c5f17b1da289b1827bfb45ba3cf456c9a43d`.

The final row in
`array_view_facts_a_resize_rule_withdraws.sg` initially looked like an
original-signature mismatch only. The selected `Array<T>.push` formal is
`&mut Array<T>`, while the indexed receiver `xs[1]` has the checker descriptor
`&Array<int>`. Reusing the complete existing scalar-index certificate removed
that row without weakening any storage-loan guard: `xs[0].push(view)` still
retained both its backing-transfer and loan-discard refusals.

That source-analysis candidate is not landable. Its exact 1187-program census
moved 21 to 20 unfinished with only the one expected row removed, but executing
the newly admitted golden exposed a backend disagreement:

* LLVM executes the program and returns its expected exit value 14;
* VM stops in `Array<int>.push` with `VM2103: addr_of_mut of non-mutable
  location`;
* emitted MIR passes `copy L17` to `push`, where `L17` is the shared reference
  produced by the indexed expression, rather than forming a mutable reborrow of
  the indexed place.

The VM guard is an existing soundness boundary: allowing `addr_of_mut` through
an arbitrary shared location would also allow real shared-reference mutation.
The rejected candidate was reset before publication. RV2-DEBT-475 owns the
missing mutable-index lowering/type contract; the D2 row remains in the
published census until that contract is fixed and both backends agree.

Evidence is retained under
`~/.cache/surge-artifacts/step7-return-source/20261009-index-receiver/`, including
the rejected candidate binary and identity, exact census/comparison, backend
outputs and emitted MIR.
