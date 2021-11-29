window.onload = function() {
    const createButton = document.getElementById('create');
    const updateButton = document.getElementById('update');
    const deleteButton = document.getElementById('delete');
    const idInput = document.getElementById('id');
    const stringInput = document.getElementById('string');
    const numInput = document.getElementById('num');

    const inbox = document.getElementById('inbox');
    const template = document.getElementById('item');

    function addMessage(updateJSON) {
        update = JSON.parse(updateJSON);
        switch (update.method) {
        case 'CREATE':
            let newItem = template.content.cloneNode(true);
            let td = newItem.querySelectorAll("td");
            td[0].textContent = update.item.id;
            td[1].textContent = update.item.num;
            td[2].textContent = update.item.string;
            inbox.appendChild(newItem);
            break;
        case 'UPDATE':
            inbox.querySelectorAll("tr").forEach(tr => {
                let td = tr.querySelectorAll("td");
                if (td[0].textContent == update.id) {
                    td[1].textContent = update.item.num;
                    td[2].textContent = update.item.string;  
                }
            })
            break;
        case 'DELETE':
            inbox.querySelectorAll("tr").forEach(tr => {
                if (tr.querySelectorAll("td")[0].textContent == update.id) {
                    inbox.removeChild(tr);
                }
            })
            break;
        }
    }

    let ws = new WebSocket('ws://localhost:8080/ws');

    ws.onmessage = function(event) {
        addMessage(event.data);
    }

    ws.onopen = function() {
        createButton.onclick = function() {
            ws.send(JSON.stringify({"method":"CREATE","item":{"string":stringInput.value,"num":Number(numInput.value)}}));
        }

        updateButton.onclick = function() {
            ws.send(JSON.stringify({"method":"UPDATE","id":idInput.value,"item":{"id":idInput.value,"string":stringInput.value,"num":Number(numInput.value)}}));
        }

        deleteButton.onclick = function() {
            ws.send(JSON.stringify({"method":"DELETE","id":idInput.value}));
        }
    }
}