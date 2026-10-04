export namespace main {
	
	export class ServerView {
	    id: string;
	    name: string;
	    region: string;
	    status: string;
	    joinable: boolean;
	    build: string;
	    buildMatch: boolean;
	    players: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.region = source["region"];
	        this.status = source["status"];
	        this.joinable = source["joinable"];
	        this.build = source["build"];
	        this.buildMatch = source["buildMatch"];
	        this.players = source["players"];
	    }
	}
	export class Settings {
	    directory: string;
	    mode: string;
	    installDir: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.directory = source["directory"];
	        this.mode = source["mode"];
	        this.installDir = source["installDir"];
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

}

