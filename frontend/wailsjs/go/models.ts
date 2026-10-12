export namespace backend {
	
	export class ConnectParams {
	    peerAddr: string;
	    password: string;
	    hashes: string[];
	    deviceId?: string;
	    workers?: number;
	    captchaMode?: string;
	    obfsMode?: string;
	    fingerprint?: string;
	    turnTcp?: boolean;
	    listen?: string;
	    maxWorkers?: number;
	    turnMode?: string;
	    turnSni?: string;
	    exclusions?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.peerAddr = source["peerAddr"];
	        this.password = source["password"];
	        this.hashes = source["hashes"];
	        this.deviceId = source["deviceId"];
	        this.workers = source["workers"];
	        this.captchaMode = source["captchaMode"];
	        this.obfsMode = source["obfsMode"];
	        this.fingerprint = source["fingerprint"];
	        this.turnTcp = source["turnTcp"];
	        this.listen = source["listen"];
	        this.maxWorkers = source["maxWorkers"];
	        this.turnMode = source["turnMode"];
	        this.turnSni = source["turnSni"];
	        this.exclusions = source["exclusions"];
	    }
	}
	export class ExclusionEntry {
	    value: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ExclusionEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.kind = source["kind"];
	    }
	}
	export class LogEntry {
	    level: string;
	    message: string;
	    time: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.message = source["message"];
	        this.time = source["time"];
	        this.count = source["count"];
	    }
	}
	export class ProfileData {
	    peer: string;
	    password: string;
	    hashes: string[];
	    workers?: number;
	    listen: string;
	    turn: string;
	    port: string;
	    device_id: string;
	    turn_tcp: boolean;
	    max_workers?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProfileData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.peer = source["peer"];
	        this.password = source["password"];
	        this.hashes = source["hashes"];
	        this.workers = source["workers"];
	        this.listen = source["listen"];
	        this.turn = source["turn"];
	        this.port = source["port"];
	        this.device_id = source["device_id"];
	        this.turn_tcp = source["turn_tcp"];
	        this.max_workers = source["max_workers"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    version: string;
	    url: string;
	    body: string;
	    pageUrl: string;
	    canInstall: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.version = source["version"];
	        this.url = source["url"];
	        this.body = source["body"];
	        this.pageUrl = source["pageUrl"];
	        this.canInstall = source["canInstall"];
	        this.size = source["size"];
	    }
	}

}

