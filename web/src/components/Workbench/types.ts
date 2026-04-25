export interface OpenFile {
  path: string;
  name: string;
  content: string;
  originalContent: string;
  language: string;
  lineEnding: 'LF' | 'CRLF';
}

export interface TerminalTab {
  id: string;
  title: string;
  sessionId: string;
}

export interface PanelState {
  visible: boolean;
  width: number; // percentage
}
