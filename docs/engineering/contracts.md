# Accepted implementation contracts

The following 20 policy groups were accepted on 2026-10-15, including the final
1 MiB resource revision. They complement the previously approved configuration,
GFM and resource contracts. Implementation issues remain open until their
code and verification criteria are satisfied; acceptance of policy is not a
conformance claim.

| Decision | Contract | Owning issues |
| --- | --- | --- |
| 1 | [Frontmatter recognition](frontmatter.md#frontmatter-recognition) | [#166](https://github.com/frithuz/mdchk/issues/166), [#167](https://github.com/frithuz/mdchk/issues/167), [#168](https://github.com/frithuz/mdchk/issues/168), [#169](https://github.com/frithuz/mdchk/issues/169), [#114](https://github.com/frithuz/mdchk/issues/114) |
| 2 | [Metadata values and corrections](frontmatter.md#metadata-values-and-corrections) | [#178](https://github.com/frithuz/mdchk/issues/178), [#179](https://github.com/frithuz/mdchk/issues/179), [#180](https://github.com/frithuz/mdchk/issues/180), [#181](https://github.com/frithuz/mdchk/issues/181), [#186](https://github.com/frithuz/mdchk/issues/186), [#33](https://github.com/frithuz/mdchk/issues/33), [#42](https://github.com/frithuz/mdchk/issues/42), [#45](https://github.com/frithuz/mdchk/issues/45), [#115](https://github.com/frithuz/mdchk/issues/115) |
| 4 | [MkDocs navigation](integrations.md#mkdocs-navigation) | [#174](https://github.com/frithuz/mdchk/issues/174), [#176](https://github.com/frithuz/mdchk/issues/176), [#177](https://github.com/frithuz/mdchk/issues/177), [#118](https://github.com/frithuz/mdchk/issues/118) |
| 5 | [Slide documents](integrations.md#slide-documents) | [#50](https://github.com/frithuz/mdchk/issues/50) |
| 6 | [Malformed syntax candidates](rule-behavior.md#malformed-syntax-candidates) | [#58](https://github.com/frithuz/mdchk/issues/58), [#72](https://github.com/frithuz/mdchk/issues/72), [#93](https://github.com/frithuz/mdchk/issues/93), [#99](https://github.com/frithuz/mdchk/issues/99), [#104](https://github.com/frithuz/mdchk/issues/104), [#110](https://github.com/frithuz/mdchk/issues/110) |
| 7 | [Trailing whitespace and hard breaks](rule-behavior.md#trailing-whitespace-and-hard-breaks) | [#61](https://github.com/frithuz/mdchk/issues/61), [#183](https://github.com/frithuz/mdchk/issues/183) |
| 8 | [ASCII punctuation](rule-behavior.md#ascii-punctuation) | [#79](https://github.com/frithuz/mdchk/issues/79), [#183](https://github.com/frithuz/mdchk/issues/183), [#215](https://github.com/frithuz/mdchk/issues/215), [#218](https://github.com/frithuz/mdchk/issues/218) |
| 9 | [Table of contents](rule-behavior.md#table-of-contents) | [#117](https://github.com/frithuz/mdchk/issues/117), [#187](https://github.com/frithuz/mdchk/issues/187) |
| 10 | [Nested Markdown examples](rule-behavior.md#nested-markdown-examples) | [#90](https://github.com/frithuz/mdchk/issues/90), [#184](https://github.com/frithuz/mdchk/issues/184) |
| 11 | [Decorative HTML images](rule-behavior.md#decorative-html-images) | [#97](https://github.com/frithuz/mdchk/issues/97) |
| 12 | [Unicode hostnames](rule-behavior.md#unicode-hostnames) | [#105](https://github.com/frithuz/mdchk/issues/105) |
| 13 | [Unused suppressions](rule-behavior.md#unused-suppressions) | [#82](https://github.com/frithuz/mdchk/issues/82), [#132](https://github.com/frithuz/mdchk/issues/132), [#123](https://github.com/frithuz/mdchk/issues/123), [#139](https://github.com/frithuz/mdchk/issues/139), [#140](https://github.com/frithuz/mdchk/issues/140) |
| 14 | [UTF-8 validation](rule-behavior.md#utf-8-validation) | [#121](https://github.com/frithuz/mdchk/issues/121), [#123](https://github.com/frithuz/mdchk/issues/123), [#128](https://github.com/frithuz/mdchk/issues/128), [#132](https://github.com/frithuz/mdchk/issues/132), [#138](https://github.com/frithuz/mdchk/issues/138), [#139](https://github.com/frithuz/mdchk/issues/139), [#140](https://github.com/frithuz/mdchk/issues/140), [#141](https://github.com/frithuz/mdchk/issues/141), [#189](https://github.com/frithuz/mdchk/issues/189) |
| 15 | [Atomic edit groups and convergence](fixes.md#atomic-edit-groups-and-convergence) | [#142](https://github.com/frithuz/mdchk/issues/142), [#143](https://github.com/frithuz/mdchk/issues/143), [#144](https://github.com/frithuz/mdchk/issues/144), [#145](https://github.com/frithuz/mdchk/issues/145), [#148](https://github.com/frithuz/mdchk/issues/148), [#67](https://github.com/frithuz/mdchk/issues/67), [#184](https://github.com/frithuz/mdchk/issues/184), [#187](https://github.com/frithuz/mdchk/issues/187) |
| 16 | [Filesystem replacement](fixes.md#filesystem-replacement) | [#146](https://github.com/frithuz/mdchk/issues/146), [#147](https://github.com/frithuz/mdchk/issues/147), [#148](https://github.com/frithuz/mdchk/issues/148), [#149](https://github.com/frithuz/mdchk/issues/149), [#195](https://github.com/frithuz/mdchk/issues/195), [#259](https://github.com/frithuz/mdchk/issues/259), [#260](https://github.com/frithuz/mdchk/issues/260) |
| 17 | [Interruption and exit precedence](results.md#interruption-and-exit-precedence) | [#123](https://github.com/frithuz/mdchk/issues/123), [#128](https://github.com/frithuz/mdchk/issues/128), [#144](https://github.com/frithuz/mdchk/issues/144), [#148](https://github.com/frithuz/mdchk/issues/148), [#189](https://github.com/frithuz/mdchk/issues/189), [#195](https://github.com/frithuz/mdchk/issues/195), [#216](https://github.com/frithuz/mdchk/issues/216), [#223](https://github.com/frithuz/mdchk/issues/223), [#249](https://github.com/frithuz/mdchk/issues/249), [#253](https://github.com/frithuz/mdchk/issues/253) |
| 18 | [Resource limits](limits.md#defaults) | [#123](https://github.com/frithuz/mdchk/issues/123), [#189](https://github.com/frithuz/mdchk/issues/189), [#216](https://github.com/frithuz/mdchk/issues/216), [#223](https://github.com/frithuz/mdchk/issues/223), [#249](https://github.com/frithuz/mdchk/issues/249), [#253](https://github.com/frithuz/mdchk/issues/253), [#144](https://github.com/frithuz/mdchk/issues/144), [#148](https://github.com/frithuz/mdchk/issues/148), [#195](https://github.com/frithuz/mdchk/issues/195) |
| 19 | [Shared result schema](results.md#shared-result-schema) | [#123](https://github.com/frithuz/mdchk/issues/123), [#128](https://github.com/frithuz/mdchk/issues/128), [#139](https://github.com/frithuz/mdchk/issues/139), [#140](https://github.com/frithuz/mdchk/issues/140), [#141](https://github.com/frithuz/mdchk/issues/141), [#144](https://github.com/frithuz/mdchk/issues/144), [#148](https://github.com/frithuz/mdchk/issues/148), [#189](https://github.com/frithuz/mdchk/issues/189), [#216](https://github.com/frithuz/mdchk/issues/216), [#223](https://github.com/frithuz/mdchk/issues/223), [#249](https://github.com/frithuz/mdchk/issues/249), [#253](https://github.com/frithuz/mdchk/issues/253), [#260](https://github.com/frithuz/mdchk/issues/260) |
| 20 | [MCP protocol and SDK](mcp.md#sdk) | [#189](https://github.com/frithuz/mdchk/issues/189), [#190](https://github.com/frithuz/mdchk/issues/190), [#191](https://github.com/frithuz/mdchk/issues/191), [#192](https://github.com/frithuz/mdchk/issues/192), [#193](https://github.com/frithuz/mdchk/issues/193), [#194](https://github.com/frithuz/mdchk/issues/194), [#195](https://github.com/frithuz/mdchk/issues/195), [#196](https://github.com/frithuz/mdchk/issues/196), [#197](https://github.com/frithuz/mdchk/issues/197), [#198](https://github.com/frithuz/mdchk/issues/198), [#199](https://github.com/frithuz/mdchk/issues/199), [#200](https://github.com/frithuz/mdchk/issues/200), [#201](https://github.com/frithuz/mdchk/issues/201), [#202](https://github.com/frithuz/mdchk/issues/202), [#203](https://github.com/frithuz/mdchk/issues/203), [#204](https://github.com/frithuz/mdchk/issues/204), [#205](https://github.com/frithuz/mdchk/issues/205), [#206](https://github.com/frithuz/mdchk/issues/206) |
| 21 | [HTTP deployment and authorization](http.md#deployment-profiles) | [#189](https://github.com/frithuz/mdchk/issues/189), [#201](https://github.com/frithuz/mdchk/issues/201), [#202](https://github.com/frithuz/mdchk/issues/202), [#203](https://github.com/frithuz/mdchk/issues/203), [#204](https://github.com/frithuz/mdchk/issues/204), [#205](https://github.com/frithuz/mdchk/issues/205), [#206](https://github.com/frithuz/mdchk/issues/206) |

## Delivery stages and prerequisites

The 87 rule issues cover analysis and configuration in Pass 1. They supply
reusable engine fixtures; they do not wait for the MCP transports or correction
passes. Project, standalone, and full transport parity remain completion gates in
[#196](https://github.com/frithuz/mdchk/issues/196),
[#200](https://github.com/frithuz/mdchk/issues/200), and
[#206](https://github.com/frithuz/mdchk/issues/206).

Complete required shared engine, catalog, source, coverage, parser and specialized
context providers before declaring a dependent rule complete. Work can be drafted
against accepted interfaces while those providers are being implemented. Native
GitHub blocker relationships track these completion prerequisites. Keep format
verification downstream of its constituent syntax work so the graph stays acyclic.

Remaining engineering work includes source-preserving metadata adapters, exact
work metering, stable AST node/property definitions, Git-ignore source discovery,
native filesystem adapters, independent MCP interoperability, and HTTP deployment
tests. These are implementation/verification tasks under the accepted policies.
