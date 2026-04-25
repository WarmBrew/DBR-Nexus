// ANSI escape sequence parser for terminal output rendering
// Returns structured data instead of raw HTML to prevent XSS

const ANSI_FG_COLORS: Record<number, string> = {
  30: '#000000', 31: '#cd3131', 32: '#0dbc79', 33: '#e5e510',
  34: '#2472c8', 35: '#bc3fbc', 36: '#11a8cd', 37: '#e5e5e5',
  90: '#666666', 91: '#f14c4c', 92: '#23d18b', 93: '#f5f543',
  94: '#3b8eea', 95: '#d670d6', 96: '#29b8db', 97: '#ffffff',
};

const ANSI_BG_COLORS: Record<number, string> = {
  40: '#000000', 41: '#cd3131', 42: '#0dbc79', 43: '#e5e510',
  44: '#2472c8', 45: '#bc3fbc', 46: '#11a8cd', 47: '#e5e5e5',
  100: '#666666', 101: '#f14c4c', 102: '#23d18b', 103: '#f5f543',
  104: '#3b8eea', 105: '#d670d6', 106: '#29b8db', 107: '#ffffff',
};

export interface StyledSegment {
  text: string;
  style?: React.CSSProperties;
}

export interface ParsedLine {
  segments: StyledSegment[];
}

function processCodes(codes: number[]): React.CSSProperties | undefined {
  const style: React.CSSProperties = {};
  let hasStyle = false;
  for (const c of codes) {
    if (c === 0) continue; // reset — handled by caller
    if (c === 1) { style.fontWeight = 'bold'; hasStyle = true; }
    else if (c === 2) { style.opacity = 0.6; hasStyle = true; } // dim
    else if (c === 3) { style.fontStyle = 'italic'; hasStyle = true; }
    else if (c === 4) { style.textDecoration = 'underline'; hasStyle = true; }
    else if (c === 7) { // reverse video — swap fg/bg
      const fg = style.color;
      const bg = style.backgroundColor;
      style.color = bg || '#1e1e1e';
      style.backgroundColor = fg || '#d4d4d4';
      hasStyle = true;
    }
    else if (c === 9) { style.textDecoration = 'line-through'; hasStyle = true; }
    else if (ANSI_FG_COLORS[c]) { style.color = ANSI_FG_COLORS[c]; hasStyle = true; }
    else if (ANSI_BG_COLORS[c]) { style.backgroundColor = ANSI_BG_COLORS[c]; hasStyle = true; }
    // 38;5;n (256-color) and 38;2;r;g;b (truecolor) — skip for now
  }
  return hasStyle ? style : undefined;
}

/** Parse a single line (no \n) with ANSI sequences → structured segments */
export function parseAnsiLine(line: string): ParsedLine {
  const segments: StyledSegment[] = [];
  let i = 0;
  let currentStyle: React.CSSProperties | undefined;
  let currentText = '';

  const flush = () => {
    if (currentText) {
      segments.push({ text: currentText, style: currentStyle });
      currentText = '';
    }
  };

  while (i < line.length) {
    // ESC [
    if (line.charCodeAt(i) === 0x1b && i + 1 < line.length && line[i + 1] === '[') {
      let j = i + 2;
      while (j < line.length && ((line[j] >= '0' && line[j] <= '9') || line[j] === ';')) j++;
      if (j < line.length && line[j] === 'm') {
        flush();
        const seq = line.substring(i + 2, j);
        if (seq === '' || seq === '0') {
          currentStyle = undefined;
        } else {
          const codes = seq.split(';').map(Number).filter(n => !isNaN(n));
          currentStyle = processCodes(codes);
        }
        i = j + 1;
      } else {
        // skip unrecognized CSI sequence until letter
        while (j < line.length && !((line[j] >= 'A' && line[j] <= 'Z') || (line[j] >= 'a' && line[j] <= 'z'))) j++;
        i = j + 1;
      }
    } else if (line.charCodeAt(i) < 0x20 && line[i] !== '\t') {
      i++; // skip control chars (except tab which is preserved)
    } else {
      currentText += line[i];
      i++;
    }
  }
  flush();
  if (segments.length === 0) segments.push({ text: '' });
  return { segments };
}

/**
 * Apply \r (carriage return) overwrite semantics to a string.
 * In a terminal, \r moves the cursor to the beginning of the line,
 * so subsequent text overwrites from the start.
 * e.g. "abc\rxyz" → "xyzc" (xyz overwrites ab, c remains)
 */
function applyCarriageReturns(text: string): string {
  const parts = text.split('\r');
  if (parts.length <= 1) return text;

  // Build the result by applying each part as an overwrite from position 0
  let result = '';
  for (const part of parts) {
    if (part === '') continue;
    // Overwrite from the start of result with the new part
    if (part.length >= result.length) {
      result = part;
    } else {
      result = part + result.substring(part.length);
    }
  }
  return result;
}

/** Split raw terminal output by \n and parse each line, handling \r properly */
export function parseAnsiOutput(raw: string): ParsedLine[] {
  const lines = raw.split('\n');
  const result: ParsedLine[] = [];

  for (const line of lines) {
    if (line.includes('\r')) {
      // Apply \r overwrite semantics, then parse ANSI
      result.push(parseAnsiLine(applyCarriageReturns(line)));
    } else {
      result.push(parseAnsiLine(line));
    }
  }

  // Remove the spurious empty line at end caused by trailing \n
  if (result.length > 1) {
    const lastLine = result[result.length - 1];
    const lastText = lastLine.segments.map(s => s.text).join('');
    if (lastText === '') {
      result.pop();
    }
  }

  return result;
}
