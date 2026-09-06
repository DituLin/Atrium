/**
 * WebSocket contract types — hand-mirrored from tech-design §9.
 */

export const WS_SCHEMA_VERSION = 1;

export const ROUTE_NAMES = ['dashboard', 'photos', 'photo', 'pair', 'connect'] as const;
export type RouteName = (typeof ROUTE_NAMES)[number];

export interface RouteState {
  name: RouteName;
  collection?: string;
  photo_id?: string;
}

export interface Envelope<T extends string, P> {
  schema_version: typeof WS_SCHEMA_VERSION;
  type: T;
  id: string;
  sent_at: string;
  payload: P;
}

/* ----------------------------------------------------------- server → client */

export const COMMAND_KINDS = ['navigate', 'show', 'refresh'] as const;
export type CommandKind = (typeof COMMAND_KINDS)[number];

export interface NavigateCommandPayload {
  route: string;
  collection?: string;
}

export interface ShowCommandPayload {
  photo_id: string;
}

export type RefreshCommandPayload = Record<string, never>;

export interface ScreenCommand {
  command_id: string;
  sequence: number;
  kind: CommandKind;
  payload: NavigateCommandPayload | ShowCommandPayload | RefreshCommandPayload;
  issued_at: string;
  expires_at: string;
}

export interface SessionReadyPayload {
  screen: { id: string; name: string };
  server_time: string;
  home_version: number;
  pending_command?: ScreenCommand;
}

export type DataChangedTopic = 'home' | 'photos' | 'nas' | 'screen';

export interface DataChangedPayload {
  topics: DataChangedTopic[];
  version: number;
}

export interface HeartbeatAckPayload {
  server_time: string;
}

export interface SessionRevokedPayload {
  reason: string;
}

export type SessionSupersededPayload = Record<string, never>;

export type ServerMessage =
  | Envelope<'session.ready', SessionReadyPayload>
  | Envelope<'screen.command', ScreenCommand>
  | Envelope<'data.changed', DataChangedPayload>
  | Envelope<'heartbeat.ack', HeartbeatAckPayload>
  | Envelope<'session.revoked', SessionRevokedPayload>
  | Envelope<'session.superseded', SessionSupersededPayload>;

export type ServerMessageType = ServerMessage['type'];

/* ----------------------------------------------------------- client → server */

export interface HeartbeatPayload {
  route: RouteState;
  applied_sequence: number;
  client_version: string;
}

export interface StatePayload {
  route: RouteState;
}

export type CommandAckStatus = 'applied' | 'failed';

export const COMMAND_ERROR_CODES = [
  'superseded',
  'photo_unavailable',
  'invalid_route',
  'invalid_payload',
  'render_failed',
] as const;
export type CommandErrorCode = (typeof COMMAND_ERROR_CODES)[number];

export interface CommandAckPayload {
  command_id: string;
  status: CommandAckStatus;
  route: RouteState;
  resource_id?: string;
  error_code?: CommandErrorCode;
}

export type ClientMessage =
  | Envelope<'heartbeat', HeartbeatPayload>
  | Envelope<'state', StatePayload>
  | Envelope<'command.ack', CommandAckPayload>;

export type ClientMessageType = ClientMessage['type'];

/* ------------------------------------------------------------- close codes */

export const WS_CLOSE = {
  normal: 1000,
  serverShutdown: 1001,
  unauthorized: 4001,
  revoked: 4002,
  superseded: 4003,
  queueOverflow: 4004,
  protocolError: 4005,
} as const;

/**
 * Compile-time exhaustiveness helper. Used by switches over command kinds and
 * message types so a new variant fails the build instead of being ignored.
 */
export function assertNever(value: never): never {
  throw new Error(`unhandled variant: ${JSON.stringify(value)}`);
}
