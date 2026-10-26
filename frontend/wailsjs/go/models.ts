export namespace recorder {
	
	export class Request {
	    url: string;
	    language: string;
	    mode: string;
	    goal: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.language = source["language"];
	        this.mode = source["mode"];
	        this.goal = source["goal"];
	    }
	}

}

export namespace runtimeenv {
	
	export class Status {
	    nodeVersion: string;
	    nodeOk: boolean;
	    playwrightOk: boolean;
	    chromiumOk: boolean;
	    pythonVersion: string;
	    pythonOk: boolean;
	    ready: boolean;
	    runtimeDir: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeVersion = source["nodeVersion"];
	        this.nodeOk = source["nodeOk"];
	        this.playwrightOk = source["playwrightOk"];
	        this.chromiumOk = source["chromiumOk"];
	        this.pythonVersion = source["pythonVersion"];
	        this.pythonOk = source["pythonOk"];
	        this.ready = source["ready"];
	        this.runtimeDir = source["runtimeDir"];
	    }
	}

}

export namespace scripts {
	
	export class Script {
	    id: string;
	    name: string;
	    language: string;
	    code: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Script(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.language = source["language"];
	        this.code = source["code"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace settings {
	
	export class Settings {
	    openaiBaseUrl: string;
	    openaiApiKey: string;
	    openaiModel: string;
	    nodePath: string;
	    pythonPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openaiBaseUrl = source["openaiBaseUrl"];
	        this.openaiApiKey = source["openaiApiKey"];
	        this.openaiModel = source["openaiModel"];
	        this.nodePath = source["nodePath"];
	        this.pythonPath = source["pythonPath"];
	    }
	}

}

