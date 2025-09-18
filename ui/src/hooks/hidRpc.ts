import { hidKeyBufferSize, KeyboardLedState, KeysDownState } from "./stores";

export const HID_RPC_MESSAGE_TYPES = {
    Handshake: 0x01,
    KeyboardReport: 0x02,
    PointerReport: 0x03,
    WheelReport: 0x04,
    KeypressReport: 0x05,
    MouseReport: 0x06,
    KeyboardMacroReport: 0x07,
    CancelKeyboardMacroReport: 0x08,
    KeyboardLedState: 0x32,
    KeysDownState: 0x33,
    KeyboardMacroState: 0x34,
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
        n & 0xFF,
    ]);
};

const fromUint16toUint8 = (n: number) => {
    if (n > 65535 || n < 0) {
        throw new Error(`Number ${n} is not within the uint16 range`);
    }

    return new Uint8Array([
        (n >> 8) & 0xFF,
        n & 0xFF,
    ]);
};

const fromUint32toUint8 = (n: number) => {
    if (n > 4294967295 || n < 0) {
        throw new Error(`Number ${n} is not within the uint32 range`);
    }

    return new Uint8Array([
        (n >> 24) & 0xFF,
        (n >> 16) & 0xFF,
        (n >> 8) & 0xFF,
        n & 0xFF,
    ]);
};

const fromInt8ToUint8 = (n: number) => {
    if (n < -128 || n > 127) {
        throw new Error(`Number ${n} is not within the int8 range`);
    }

    return n & 0xFF;
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

export interface KeyboardMacroStep extends KeysDownState {
    delay: number;
}

export class KeyboardMacroReportMessage extends RpcMessage {
    isPaste: boolean;
    stepCount: number;
    steps: KeyboardMacroStep[];

    KEYS_LENGTH = hidKeyBufferSize;

    constructor(isPaste: boolean, stepCount: number, steps: KeyboardMacroStep[]) {
        super(HID_RPC_MESSAGE_TYPES.KeyboardMacroReport);
        this.isPaste = isPaste;
        this.stepCount = stepCount;
        this.steps = steps;
    }

    marshal(): Uint8Array {
        // validate if length is correct
        if (this.stepCount !== this.steps.length) {
            throw new Error(`Length ${this.stepCount} is not equal to the number of steps ${this.steps.length}`);
        }

        const data = new Uint8Array(this.stepCount * 9 + 6);
        data.set(new Uint8Array([
            this.messageType,
            this.isPaste ? 1 : 0,
            ...fromUint32toUint8(this.stepCount),
        ]), 0);

        for (let i = 0; i < this.stepCount; i++) {
            const step = this.steps[i];
            if (!withinUint8Range(step.modifier)) {
                throw new Error(`Modifier ${step.modifier} is not within the uint8 range`);
            }

            // Ensure the keys are within the KEYS_LENGTH range
            const keys = step.keys;
            if (keys.length > this.KEYS_LENGTH) {
                throw new Error(`Keys ${keys} is not within the hidKeyBufferSize range`);
            } else if (keys.length < this.KEYS_LENGTH) {
                keys.push(...Array(this.KEYS_LENGTH - keys.length).fill(0));
            }

            for (const key of keys) {
                if (!withinUint8Range(key)) {
                    throw new Error(`Key ${key} is not within the uint8 range`);
                }
            }

            const macroBinary = new Uint8Array([
                step.modifier,
                ...keys,
                ...fromUint16toUint8(step.delay),
            ]);
            const offset = 6 + i * 9;

            
            data.set(macroBinary, offset);
        }

        return data;
    }
}

export class KeyboardMacroStateMessage extends RpcMessage {
    state: boolean;
    isPaste: boolean;

    constructor(state: boolean, isPaste: boolean) {
        super(HID_RPC_MESSAGE_TYPES.KeyboardMacroState);
        this.state = state;
        this.isPaste = isPaste;
    }

    marshal(): Uint8Array {
        return new Uint8Array([
            this.messageType,
            this.state ? 1 : 0,
            this.isPaste ? 1 : 0,
        ]);
    }

    public static unmarshal(data: Uint8Array): KeyboardMacroStateMessage | undefined {
        if (data.length < 1) {
            throw new Error(`Invalid keyboard macro state report message length: ${data.length}`);
        }

        return new KeyboardMacroStateMessage(data[0] === 1, data[1] === 1);
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

export class CancelKeyboardMacroReportMessage extends RpcMessage {

    constructor() {
        super(HID_RPC_MESSAGE_TYPES.CancelKeyboardMacroReport);
    }

    marshal(): Uint8Array {
        return new Uint8Array([this.messageType]);
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
    [HID_RPC_MESSAGE_TYPES.KeyboardMacroReport]: KeyboardMacroReportMessage,
    [HID_RPC_MESSAGE_TYPES.CancelKeyboardMacroReport]: CancelKeyboardMacroReportMessage,
    [HID_RPC_MESSAGE_TYPES.KeyboardMacroState]: KeyboardMacroStateMessage,
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