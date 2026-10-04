export namespace main {
	
	export class ServerView {
	    id: string;
	    key: string;
	    name: string;
	    region: string;
	    status: string;
	    joinable: boolean;
	    build: string;
	    buildMatch: boolean;
	    players: number;
	    reachability: string;
	    pingMs: number;
	    favorite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.name = source["name"];
	        this.region = source["region"];
	        this.status = source["status"];
	        this.joinable = source["joinable"];
	        this.build = source["build"];
	        this.buildMatch = source["buildMatch"];
	        this.players = source["players"];
	        this.reachability = source["reachability"];
	        this.pingMs = source["pingMs"];
	        this.favorite = source["favorite"];
	    }
	}
	export class Settings {
	    directory: string;
	    mode: string;
	    installDir: string;
	    favorites: string[];
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.directory = source["directory"];
	        this.mode = source["mode"];
	        this.installDir = source["installDir"];
	        this.favorites = source["favorites"];
	    }
	}
	export class StatusView {
	    serverId: string;
	    serverName: string;
	    mode: string;
	    beaconAge: number;
	    connected: boolean;
	    upKB: number;
	    downKB: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.mode = source["mode"];
	        this.beaconAge = source["beaconAge"];
	        this.connected = source["connected"];
	        this.upKB = source["upKB"];
	        this.downKB = source["downKB"];
	        this.error = source["error"];
	    }
	}
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    url: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.url = source["url"];
	        this.available = source["available"];
	    }
	}

}

