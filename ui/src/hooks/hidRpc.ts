import { KeyboardLedState, KeysDownState } from "./stores";

export const HID_RPC_MESSAGE_TYPES = {
    Handshake: 0x01,
    KeyboardReport: 0x02,
    PointerReport: 0x03,
    WheelReport: 0x04,
    KeypressReport: 0x05,
    MouseReport: 0x06,
    KeyboardLedState: 0x32,
    KeysDownState: 0x33,
}

export type HidRpcMessageType = typeof HID_RPC_MESSAGE_TYPES[keyof typeof HID_RPC_MESSAGE_TYPES];

export const HID_RPC_VERSION = 0x01;

const withinUint8Range = (value: number) => {
    return value >= 0 && value <= 255;
};

const fromInt32toUint8 = (n: number) => {
    if (n !== n >> 0) {
        throw new Error(`Number ${n} is not within the int32 range`);
    }

    return new Uint8Array([
        (n >> 24) & 0xFF,
        (n >> 16) & 0xFF,
        (n >> 8) & 0xFF,
        (n >> 0) & 0xFF,
    ]);
};

const fromInt8ToUint8 = (n: number) => {
    if (n < -128 || n > 127) {
        throw new Error(`Number ${n} is not within the int8 range`);
    }

    return (n >> 0) & 0xFF;
};

const keyboardLedStateMasks = {
    num_lock: 1 << 0,
    caps_lock: 1 << 1,
    scroll_lock: 1 << 2,
    compose: 1 << 3,
    kana: 1 << 4,
    shift: 1 << 6,
}

export class RpcMessage {
    messageType: HidRpcMessageType;

    constructor(messageType: HidRpcMessageType) {
        this.messageType = messageType;
    }

    marshal(): Uint8Array {
        throw new Error("Not implemented");
    }

    public static unmarshal(_data: Uint8Array): RpcMessage | undefined {
        throw new Error("Not implemented");
    }
}

export class HandshakeMessage extends RpcMessage {
    version: number;

    constructor(version: number) {
        super(HID_RPC_MESSAGE_TYPES.Handshake);
        this.version = version;
    }

    marshal(): Uint8Array {
        return new Uint8Array([this.messageType, this.version]);
    }

    public static unmarshal(data: Uint8Array): HandshakeMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid handshake message length: ${data.length}`);
        }

        return new HandshakeMessage(data[0]);
    }
}

export class KeypressReportMessage extends RpcMessage {
    private _key = 0;
    private _press = false;

    get key(): number {
        return this._key;
    }

    set key(value: number) {
        if (!withinUint8Range(value)) {
            throw new Error(`Key ${value} is not within the uint8 range`);
        }

        this._key = value;
    }

    get press(): boolean {
        return this._press;
    }

    set press(value: boolean) {
        this._press = value;
    }

    constructor(key: number, press: boolean) {
        super(HID_RPC_MESSAGE_TYPES.KeypressReport);
        this.key = key;
        this.press = press;
    }

    marshal(): Uint8Array {
        return new Uint8Array([
            this.messageType,
            this.key,
            this.press ? 1 : 0,
        ]);
    }

    public static unmarshal(data: Uint8Array): KeypressReportMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid keypress report message length: ${data.length}`);
        }

        return new KeypressReportMessage(data[0], data[1] === 1);
    }
}

export class KeyboardReportMessage extends RpcMessage {
    private _keys: number[] = [];
    private _modifier = 0;

    get keys(): number[] {
        return this._keys;
    }

    set keys(value: number[]) {
        value.forEach((k) => {
            if (!withinUint8Range(k)) {
                throw new Error(`Key ${k} is not within the uint8 range`);
            }
        });

        this._keys = value;
    }

    get modifier(): number {
        return this._modifier;
    }

    set modifier(value: number) {
        if (!withinUint8Range(value)) {
            throw new Error(`Modifier ${value} is not within the uint8 range`);
        }

        this._modifier = value;
    }

    constructor(keys: number[], modifier: number) {
        super(HID_RPC_MESSAGE_TYPES.KeyboardReport);
        this.keys = keys;
        this.modifier = modifier;
    }

    marshal(): Uint8Array {
        return new Uint8Array([
            this.messageType,
            this.modifier,
            ...this.keys,
        ]);
    }

    public static unmarshal(data: Uint8Array): KeyboardReportMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid keyboard report message length: ${data.length}`);
        }

        return new KeyboardReportMessage(Array.from(data.slice(1)), data[0]);
    }
}

export class KeyboardLedStateMessage extends RpcMessage {
    keyboardLedState: KeyboardLedState;

    constructor(keyboardLedState: KeyboardLedState) {
        super(HID_RPC_MESSAGE_TYPES.KeyboardLedState);
        this.keyboardLedState = keyboardLedState;
    }

    public static unmarshal(data: Uint8Array): KeyboardLedStateMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid keyboard led state message length: ${data.length}`);
        }

        const s = data[0];

        const state = {
            num_lock: (s & keyboardLedStateMasks.num_lock) !== 0,
            caps_lock: (s & keyboardLedStateMasks.caps_lock) !== 0,
            scroll_lock: (s & keyboardLedStateMasks.scroll_lock) !== 0,
            compose: (s & keyboardLedStateMasks.compose) !== 0,
            kana: (s & keyboardLedStateMasks.kana) !== 0,
            shift: (s & keyboardLedStateMasks.shift) !== 0,
        } as KeyboardLedState;

        return new KeyboardLedStateMessage(state);
    }
}

export class KeysDownStateMessage extends RpcMessage {
    keysDownState: KeysDownState;

    constructor(keysDownState: KeysDownState) {
        super(HID_RPC_MESSAGE_TYPES.KeysDownState);
        this.keysDownState = keysDownState;
    }

    public static unmarshal(data: Uint8Array): KeysDownStateMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid keys down state message length: ${data.length}`);
        }

        return new KeysDownStateMessage({
            modifier: data[0],
            keys: Array.from(data.slice(1))
        });
    }
}

export class PointerReportMessage extends RpcMessage {
    x: number;
    y: number;
    buttons: number;

    constructor(x: number, y: number, buttons: number) {
        super(HID_RPC_MESSAGE_TYPES.PointerReport);
        this.x = x;
        this.y = y;
        this.buttons = buttons;
    }

    marshal(): Uint8Array {
        return new Uint8Array([
            this.messageType,
            ...fromInt32toUint8(this.x),
            ...fromInt32toUint8(this.y),
            this.buttons,
        ]);
    }
}

export class MouseReportMessage extends RpcMessage {
    dx: number;
    dy: number;
    buttons: number;

    constructor(dx: number, dy: number, buttons: number) {
        super(HID_RPC_MESSAGE_TYPES.MouseReport);
        this.dx = dx;
        this.dy = dy;
        this.buttons = buttons;
    }

    marshal(): Uint8Array {
        return new Uint8Array([
            this.messageType,
            fromInt8ToUint8(this.dx),
            fromInt8ToUint8(this.dy),
            this.buttons,
        ]);
    }
}

export const messageRegistry = {
    [HID_RPC_MESSAGE_TYPES.Handshake]: HandshakeMessage,
    [HID_RPC_MESSAGE_TYPES.KeysDownState]: KeysDownStateMessage,
    [HID_RPC_MESSAGE_TYPES.KeyboardLedState]: KeyboardLedStateMessage,
    [HID_RPC_MESSAGE_TYPES.KeyboardReport]: KeyboardReportMessage,
    [HID_RPC_MESSAGE_TYPES.KeypressReport]: KeypressReportMessage,
}

export const unmarshalHidRpcMessage = (data: Uint8Array): RpcMessage | undefined => {
    if (data.length < 1) {
        throw new Error(`Invalid HID RPC message length: ${data.length}`);
    }

    const payload = data.slice(1);

    const messageType = data[0];
    if (!(messageType in messageRegistry)) {
        throw new Error(`Unknown HID RPC message type: ${messageType}`);
    }

    return messageRegistry[messageType].unmarshal(payload);
};