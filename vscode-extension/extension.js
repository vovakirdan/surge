const vscode = require('vscode');
const cp = require('child_process');

const SURGE_SOURCE = 'surge';
const DEFAULT_DEBOUNCE_MS = 500;

class SurgeAnalyzer {
    constructor(context, diagnosticCollection) {
        this.context = context;
        this.diagnosticCollection = diagnosticCollection;
        this.debounceHandles = new Map();
        this.output = vscode.window.createOutputChannel('Surge Analyzer');
        this.available = undefined;
        this.notifiedMissing = false;
        this.commandPath = this.getExecutablePath();

        context.subscriptions.push(this.output);
        context.subscriptions.push(
            vscode.workspace.onDidChangeConfiguration((event) => {
                if (event.affectsConfiguration('surge.analyzer.executablePath')) {
                    this.commandPath = this.getExecutablePath();
                    this.available = undefined;
                    this.notifiedMissing = false;
                    this.output.appendLine(`[info] Surge executable path changed to "${this.commandPath}".`);
                    vscode.workspace.textDocuments.forEach((doc) => this.trigger(doc));
                }
            })
        );
    }

    getExecutablePath() {
        const config = vscode.workspace.getConfiguration('surge');
        const configured = config.get('analyzer.executablePath', 'surge');
        if (typeof configured !== 'string' || configured.trim().length === 0) {
            return 'surge';
        }
        return configured.trim();
    }

    trigger(document) {
        if (!document || document.languageId !== 'surge') {
            return;
        }
        if (this.available === false) {
            return;
        }
        const key = document.uri.toString();
        const existing = this.debounceHandles.get(key);
        if (existing) {
            clearTimeout(existing);
        }
        const handle = setTimeout(() => {
            this.debounceHandles.delete(key);
            this.runAnalysis(document);
        }, DEFAULT_DEBOUNCE_MS);
        this.debounceHandles.set(key, handle);
    }

    async runAnalysis(document) {
        if (this.available === false) {
            return;
        }
        const text = document.getText();
        const version = document.version;
        const result = await this.invokeSurge(text);
        if (!result) {
            return;
        }
        if (document.version !== version || document.isClosed) {
            return;
        }
        const diagnostics = this.parseDiagnostics(result.stderr, document);
        this.diagnosticCollection.set(document.uri, diagnostics);
    }

    invokeSurge(sourceText) {
        return new Promise((resolve) => {
            const proc = cp.spawn(this.commandPath, ['sema', '-'], { stdio: ['pipe', 'pipe', 'pipe'] });
            let stderr = '';
            let stdout = '';
            let finished = false;

            const finish = (value) => {
                if (!finished) {
                    finished = true;
                    resolve(value);
                }
            };

            proc.on('error', (err) => {
                if (err && err.code === 'ENOENT') {
                    this.available = false;
                    if (!this.notifiedMissing) {
                        this.notifiedMissing = true;
                        vscode.window.showWarningMessage('Surge executable not found. Semantic analysis is disabled.');
                    }
                    this.diagnosticCollection.clear();
                    finish(null);
                    return;
                }
                this.output.appendLine(`[error] Failed to run surge: ${err.message}`);
                finish(null);
            });

            proc.stdout.on('data', (chunk) => {
                stdout += chunk.toString();
            });

            proc.stderr.on('data', (chunk) => {
                stderr += chunk.toString();
            });

            proc.on('close', () => {
                if (this.available !== false) {
                    this.available = true;
                }
                finish({ stdout, stderr });
            });

            proc.stdin.setDefaultEncoding('utf-8');
            proc.stdin.write(sourceText);
            proc.stdin.end();
        });
    }

    parseDiagnostics(stderr, document) {
        if (!stderr) {
            return [];
        }
        const diagnostics = [];
        const regex = /^(.*?):(\d+):(\d+):\s+(error|warning|note):\s+(.*)$/;
        const lines = stderr.split(/\r?\n/);
        for (const line of lines) {
            const match = regex.exec(line);
            if (!match) {
                continue;
            }
            const lineIndex = parseInt(match[2], 10) - 1;
            const columnIndex = parseInt(match[3], 10) - 1;
            if (!Number.isFinite(lineIndex) || lineIndex < 0 || lineIndex >= document.lineCount) {
                continue;
            }
            const severity = this.mapSeverity(match[4]);
            const range = this.createRange(document, lineIndex, columnIndex);
            const message = match[5].trim();
            const diagnostic = new vscode.Diagnostic(range, message, severity);
            diagnostic.source = SURGE_SOURCE;
            diagnostics.push(diagnostic);
        }
        return diagnostics;
    }

    mapSeverity(kind) {
        switch (kind) {
            case 'warning':
                return vscode.DiagnosticSeverity.Warning;
            case 'note':
                return vscode.DiagnosticSeverity.Information;
            case 'error':
            default:
                return vscode.DiagnosticSeverity.Error;
        }
    }

    createRange(document, lineIndex, columnIndex) {
        try {
            const line = document.lineAt(lineIndex);
            const startCol = Math.max(0, Math.min(columnIndex, line.text.length));
            let endCol = startCol;
            if (line.text.length > startCol) {
                const fragment = line.text.slice(startCol);
                const match = fragment.match(/^[^\s]+/);
                if (match && match[0].length > 0) {
                    endCol = startCol + match[0].length;
                } else {
                    endCol = startCol + 1;
                }
            }
            return new vscode.Range(
                new vscode.Position(lineIndex, startCol),
                new vscode.Position(lineIndex, Math.min(endCol, line.text.length))
            );
        } catch (err) {
            return new vscode.Range(new vscode.Position(lineIndex, 0), new vscode.Position(lineIndex, 0));
        }
    }
}

/**
 * @param {vscode.ExtensionContext} context
 */
function activate(context) {
    const diagnosticCollection = vscode.languages.createDiagnosticCollection(SURGE_SOURCE);
    context.subscriptions.push(diagnosticCollection);

    const analyzer = new SurgeAnalyzer(context, diagnosticCollection);

    context.subscriptions.push(
        vscode.workspace.onDidOpenTextDocument((doc) => analyzer.trigger(doc))
    );
    context.subscriptions.push(
        vscode.workspace.onDidChangeTextDocument((event) => analyzer.trigger(event.document))
    );
    context.subscriptions.push(
        vscode.workspace.onDidSaveTextDocument((doc) => analyzer.trigger(doc))
    );
    context.subscriptions.push(
        vscode.workspace.onDidCloseTextDocument((doc) => diagnosticCollection.delete(doc.uri))
    );

    vscode.workspace.textDocuments.forEach((doc) => analyzer.trigger(doc));
}

function deactivate() {}

module.exports = {
    activate,
    deactivate,
};
